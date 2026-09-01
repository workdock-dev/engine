repo=$(find /home/${USER}/workspace -type d -name .git -print -quit)

[ -n "$repo" ] || exit 0

cd "$(dirname "$repo")" || exit 1

output=$(gh pr view --json number,url,headRefName,headRefOid 2>&1)
exit_code=$?

if [ "$exit_code" -eq 0 ]; then
    printf '%s\n' "$output"
elif [[ "$output" == *"no pull requests found for branch"* ]]; then
    exit 0
else
    printf '%s\n' "$output" >&2
    exit "$exit_code"
fi
