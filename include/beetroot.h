#ifndef BEETROOT_H
#define BEETROOT_H
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/* Experimental ABI v1. See docs/C_LIBRARY.md for lifetime/threading rules.
 * Strings passed in are borrowed UTF-8 for the duration of the call.
 * All returned char* buffers must be released with BrFree, including errors.
 * Existing requests remain valid after BrClose(client).
 * The embedded Go runtime must remain mapped until process exit. */
uint32_t BrVersion(void);
uint64_t BrOpen(const char *config_path, const char *downloads_dir, char **error_out);
int BrClose(uint64_t client);
uint64_t BrRequest(uint64_t client);
char *BrSearch(uint64_t request, const char *query);
char *BrDownload(uint64_t request, const char *track_url);
char *BrProgress(uint64_t request);
int BrCancel(uint64_t request);
int BrCloseRequest(uint64_t request);
void BrFree(char *buffer);

#ifdef __cplusplus
}
#endif
#endif
