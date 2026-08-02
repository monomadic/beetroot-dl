package main

import (
	"fmt"
	"os"
)

// Completion scripts are embedded and printed via --completions so the single
// release binary is all a user needs:
//
//	beetroot-dl --completions zsh > "${fpath[1]}/_beetroot-dl"
//	beetroot-dl --completions bash > ~/.local/share/bash-completion/completions/beetroot-dl

const bashCompletions = `# bash completion for beetroot-dl
_beetroot_dl() {
    local cur prev
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"

    case "$prev" in
        --config|-config)
            COMPREPLY=( $(compgen -f -- "$cur") )
            return
            ;;
        --completions|-completions)
            COMPREPLY=( $(compgen -W "bash zsh" -- "$cur") )
            return
            ;;
    esac

    if [[ "$cur" == -* ]]; then
        COMPREPLY=( $(compgen -W "-i --interactive -v --verbose -q --quiet --config --completions --version -h --help" -- "$cur") )
        return
    fi

    # Positional arguments are URLs, search queries, or .txt files of URLs;
    # filenames are the only thing worth completing.
    COMPREPLY=( $(compgen -f -- "$cur") )
}
complete -F _beetroot_dl beetroot-dl
`

const zshCompletions = `#compdef beetroot-dl
# zsh completion for beetroot-dl

_beetroot-dl() {
    _arguments -s \
        '(-i --interactive)'{-i,--interactive}'[keep prompting for more URLs after finishing]' \
        '(-v --verbose)'{-v,--verbose}'[report each step (fetch, download, tag) to stderr]' \
        '(-q --quiet)'{-q,--quiet}'[suppress progress bars]' \
        '--config[use this config file instead of the default]:config file:_files' \
        '--completions[print a shell completion script to stdout]:shell:(bash zsh)' \
        '--version[print version and exit]' \
        '(-h --help)'{-h,--help}'[show help]' \
        '*:url, query, or .txt file of urls:_files -g "*.txt"'
}

_beetroot-dl "$@"
`

// printCompletions writes the requested script to stdout — the one case
// besides downloaded paths where stdout is the right stream, since the whole
// point is piping it into a file. Returns an error for unknown shells.
func printCompletions(shell string) error {
	switch shell {
	case "bash":
		fmt.Print(bashCompletions)
	case "zsh":
		fmt.Print(zshCompletions)
	default:
		return fmt.Errorf("unsupported shell %q (supported: bash, zsh)", shell)
	}
	return nil
}

func runCompletions(shell string) {
	if err := printCompletions(shell); err != nil {
		fmt.Fprintf(os.Stderr, "beetroot-dl: %s\n", err)
		os.Exit(2)
	}
	os.Exit(0)
}
