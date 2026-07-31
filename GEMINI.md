# Project Agent Rules & Mandates

## Git Commit Protocol
- **Explicit Approval Required**: You are ONLY allowed to execute `git commit` when the user explicitly grants approval for the commit in the conversation.
- **No Commit Messages**: You must NEVER supply a commit message (do NOT use `-m`, `-F`, `--message`, or specify any message text). A Git hook (`prepare-commit-msg`) automatically generates commit messages.
- **Non-Interactive Execution**: To prevent Git from hanging waiting for interactive editor input in a non-TTY environment, ALWAYS execute commits non-interactively using `--no-edit` and/or `GIT_EDITOR=true` (e.g., `git commit --no-edit` or `GIT_EDITOR=true git commit --no-edit`).
