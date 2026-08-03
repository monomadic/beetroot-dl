package beatport

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net/url"
	"os"
	"path"
	"sync"
	"time"
)

const (
	clientId      = "ryZ8LuyQVPqbK2mBX2Hwt4qSMtnWuTYSqBPO92yQ"
	tokenEndpoint = "/auth/o/token/"
	authEndpoint  = "/auth/o/authorize/?client_id=" + clientId + "&response_type=code"
	loginEndpoint = "/auth/login/"
)

var (
	ErrInvalidAuthorizationCode = errors.New("invalid authorization code")
	ErrInvalidSessionCookie     = errors.New("invalid session cookie")
	ErrLoginIDMismatch          = errors.New("login id does not match")
)

type Auth struct {
	username  string
	password  string
	tokenPair *tokenPair
	cacheFile string

	// mutex guards tokenPair. It is only ever held around in-memory access —
	// never across a network call. Holding it over a refresh would deadlock:
	// refresh issues a request, and every request reads the token back through
	// AccessToken, whose RLock cannot be taken while the write lock is held
	// (Go's RWMutex is not reentrant).
	mutex sync.RWMutex

	// refreshMutex serialises token refreshes, so a burst of workers that all
	// notice an expiring token produces one refresh rather than one each.
	refreshMutex sync.Mutex
}

type tokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	LoginID      string `json:"login_id"`
	IssuedAt     int64  `json:"issued_at"`
}

func NewAuth(username, password, cacheFile string) *Auth {
	return &Auth{
		username:  username,
		password:  password,
		cacheFile: cacheFile,
	}
}

func (a *Auth) LoadCache() error {
	data, err := os.ReadFile(a.cacheFile)
	if err != nil {
		return fmt.Errorf("failed to read token file: %w", err)
	}

	var loadedToken tokenPair
	if err := json.Unmarshal(data, &loadedToken); err != nil {
		return fmt.Errorf("failed to unmarshal token data: %w", err)
	}

	if loadedToken.LoginID != a.loginId() {
		return ErrLoginIDMismatch
	}

	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.tokenPair = &loadedToken

	return nil
}

// storeToken installs a freshly issued token pair and returns it marshalled,
// so the caller can write the cache file without holding the lock.
func (a *Auth) storeToken(tp *tokenPair, loginID string) ([]byte, error) {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	tp.IssuedAt = time.Now().Unix()
	tp.LoginID = loginID
	a.tokenPair = tp

	data, err := json.MarshalIndent(tp, "", " ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tokenPair: %w", err)
	}
	return data, nil
}

// writeCache persists an already-marshalled token pair. Called outside the
// lock — the file write must not block other workers reading the token.
func (a *Auth) writeCache(data []byte) error {
	if err := os.MkdirAll(path.Dir(a.cacheFile), 0700); err != nil {
		return fmt.Errorf("could not create folder for cache file: %w", err)
	}

	if err := os.WriteFile(a.cacheFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write token to cache: %w", err)
	}

	return nil
}

// Check refreshes the token if it is within 5 minutes of expiring. The expiry
// is re-tested after taking refreshMutex: without that, every worker that
// queued on it would refresh again in turn, each one invalidating the token the
// previous worker just fetched.
func (a *Auth) Check(inst *Beatport) error {
	if !a.expiringSoon() {
		return nil
	}

	a.refreshMutex.Lock()
	defer a.refreshMutex.Unlock()

	if !a.expiringSoon() {
		// Another worker refreshed while we waited for the lock.
		return nil
	}

	fmt.Fprintln(os.Stderr, "Refreshing token")
	if _, err := a.refresh(inst); err != nil {
		if err = a.Init(inst); err != nil {
			return fmt.Errorf("invalid token and authorization error: %w", err)
		}
	}
	return nil
}

func (a *Auth) expiringSoon() bool {
	a.mutex.RLock()
	defer a.mutex.RUnlock()
	return a.expiringSoonLocked()
}

// expiringSoonLocked requires the caller to hold the mutex (either mode).
func (a *Auth) expiringSoonLocked() bool {
	if a.tokenPair == nil {
		return true
	}
	return time.Now().Unix()+300 >= a.tokenPair.IssuedAt+a.tokenPair.ExpiresIn
}

// AccessToken returns the current bearer token, or "" when there is none yet.
func (a *Auth) AccessToken() string {
	a.mutex.RLock()
	defer a.mutex.RUnlock()
	if a.tokenPair == nil {
		return ""
	}
	return a.tokenPair.AccessToken
}

// Invalidate marks the current token as expired, so the next Check refreshes.
func (a *Auth) Invalidate() {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if a.tokenPair != nil {
		a.tokenPair.IssuedAt = 0
	}
}

func (a *Auth) Init(inst *Beatport) error {
	fmt.Fprintln(os.Stderr, "Logging in")
	sessionId, err := a.login(inst)
	if err != nil {
		return fmt.Errorf("login: %v", err)
	}
	authorizationCode, err := a.authorize(inst, sessionId)
	if err != nil {
		return fmt.Errorf("authorize: %v", err)
	}
	if err := a.issue(inst, authorizationCode); err != nil {
		return fmt.Errorf("issue token: %v", err)
	}
	return nil
}

func (a *Auth) refresh(inst *Beatport) (*tokenPair, error) {
	a.mutex.RLock()
	if a.tokenPair == nil {
		a.mutex.RUnlock()
		return nil, errors.New("no token to refresh")
	}
	refreshToken := a.tokenPair.RefreshToken
	loginId := a.tokenPair.LoginID
	a.mutex.RUnlock()

	payload := map[string]string{
		"client_id":     clientId,
		"refresh_token": refreshToken,
		"grant_type":    "refresh_token",
	}

	res, err := inst.fetch("POST", tokenEndpoint, payload, "application/x-www-form-urlencoded")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	response := &tokenPair{}
	if err = json.NewDecoder(res.Body).Decode(response); err != nil {
		return nil, err
	}

	data, err := a.storeToken(response, loginId)
	if err != nil {
		return nil, err
	}
	if err := a.writeCache(data); err != nil {
		return nil, err
	}

	return response, nil
}

func (a *Auth) issue(inst *Beatport, code string) error {
	payload := map[string]string{
		"client_id": clientId,
	}

	if code != "" {
		payload["grant_type"] = "authorization_code"
		payload["code"] = code
	} else {
		payload["grant_type"] = "password"
		payload["username"] = a.username
		payload["password"] = a.password
	}

	res, err := inst.fetch("POST", tokenEndpoint, payload, "application/x-www-form-urlencoded")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	response := &tokenPair{}
	if err = json.NewDecoder(res.Body).Decode(response); err != nil {
		return err
	}

	data, err := a.storeToken(response, a.loginId())
	if err != nil {
		return err
	}
	return a.writeCache(data)
}

func (a *Auth) authorize(inst *Beatport, sessionId string) (string, error) {
	inst.headers["cookie"] = fmt.Sprintf("sessionid=%s", sessionId)
	res, err := inst.fetch("GET", authEndpoint, nil, "")
	delete(inst.headers, "cookie")
	if err != nil {
		return "", err
	}

	redirectUrl := res.Header.Get("Location")
	parsedUrl, err := url.Parse(redirectUrl)
	if err != nil {
		return "", err
	}

	query, _ := url.ParseQuery(parsedUrl.RawQuery)
	code := query.Get("code")
	if code != "" {
		return code, nil
	}

	return "", ErrInvalidAuthorizationCode
}

func (a *Auth) login(inst *Beatport) (string, error) {
	payload := map[string]string{
		"username": a.username,
		"password": a.password,
	}

	res, err := inst.fetch("POST", loginEndpoint, payload, "application/json")
	if err != nil {
		return "", err
	}
	cookies := res.Cookies()
	for _, cookie := range cookies {
		if cookie.Name == "sessionid" {
			return cookie.Value, nil
		}
	}
	return "", ErrInvalidSessionCookie
}

func (a *Auth) loginId() string {
	hash := fnv.New64a()
	data := fmt.Sprintf("%s:%s", a.username, a.password)
	hash.Write([]byte(data))
	hashBytes := hash.Sum(nil)
	return hex.EncodeToString(hashBytes)
}
