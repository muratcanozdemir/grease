#!/usr/bin/env bash
#
# pin-actions.sh — resolve the current commit SHA for each GitHub Action used in
# the workflows, so pins can be refreshed deliberately rather than guessed.
#
# Why this exists: SHA-pinning actions is a supply-chain requirement (a tag can
# be silently repointed at malicious code; a SHA cannot). But a SHA is only
# trustworthy if it was resolved from the authoritative source. This script
# resolves each action's chosen release tag to its commit SHA via the GitHub
# API, so you can paste a verified value into the workflow rather than trusting
# one copied from a blog.
#
# Usage:
#   GITHUB_TOKEN=ghp_xxx scripts/pin-actions.sh
#
# A token is strongly recommended (unauthenticated API access is rate-limited to
# 60 req/hour per IP). With a token you get 5000/hour.
#
# The mapping below is the set of actions the workflows depend on, pinned to a
# specific release tag. To move to a newer release, bump the tag here, re-run,
# and update the SHA + comment in the workflow files.

set -euo pipefail

# action repo -> release tag to pin to. Keep in sync with .github/workflows/*.
declare -A ACTIONS=(
  ["actions/checkout"]="v4.2.2"
  ["actions/setup-go"]="v5.5.0"
  ["actions/upload-artifact"]="v4.6.2"
  ["actions/download-artifact"]="v4.3.0"
  ["softprops/action-gh-release"]="v2.2.2"
)

auth_header=()
if [ -n "${GITHUB_TOKEN:-}" ]; then
  auth_header=(-H "Authorization: Bearer ${GITHUB_TOKEN}")
else
  echo "warning: GITHUB_TOKEN not set; you may hit the unauthenticated rate limit." >&2
fi

api() {
  curl -fsSL "${auth_header[@]}" -H "Accept: application/vnd.github+json" "$@"
}

resolve_sha() {
  local repo="$1" tag="$2"
  # Get the ref object for the tag.
  local ref objtype objsha
  ref="$(api "https://api.github.com/repos/${repo}/git/refs/tags/${tag}")"
  objtype="$(printf '%s' "$ref" | python3 -c "import sys,json;print(json.load(sys.stdin)['object']['type'])")"
  objsha="$(printf '%s' "$ref" | python3 -c "import sys,json;print(json.load(sys.stdin)['object']['sha'])")"
  if [ "$objtype" = "tag" ]; then
    # Annotated tag: dereference to the commit it wraps.
    api "https://api.github.com/repos/${repo}/git/tags/${objsha}" \
      | python3 -c "import sys,json;print(json.load(sys.stdin)['object']['sha'])"
  else
    printf '%s\n' "$objsha"
  fi
}

echo "Resolved action pins (paste into .github/workflows/*.yml):"
echo
for repo in "${!ACTIONS[@]}"; do
  tag="${ACTIONS[$repo]}"
  sha="$(resolve_sha "$repo" "$tag")"
  printf '  uses: %s@%s # %s\n' "$repo" "$sha" "$tag"
done
echo
echo "Verify each SHA exists by visiting https://github.com/<repo>/commit/<sha>."