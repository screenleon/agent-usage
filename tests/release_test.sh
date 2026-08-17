#!/usr/bin/env bash
# Contract for make release: linux/amd64 + linux/arm64 artifacts and SHA256SUMS.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

amd64_name=agent-usage-linux-amd64
arm64_name=agent-usage-linux-arm64
workflow="$root/.github/workflows/release.yml"
ci_workflow="$root/.github/workflows/ci.yml"
makefile="$root/Makefile"

fail() {
	echo "release_test: $*" >&2
	exit 1
}

# Public contract lives in the test, not shared with the Makefile, so a
# renamed artifact or flipped GOARCH in the recipe is a failure.
grep -q 'GOARCH=amd64' "$makefile" || fail "Makefile missing GOARCH=amd64"
grep -q 'GOARCH=arm64' "$makefile" || fail "Makefile missing GOARCH=arm64"
grep -q "$amd64_name" "$makefile" || fail "Makefile missing $amd64_name"
grep -q "$arm64_name" "$makefile" || fail "Makefile missing $arm64_name"

make -C "$root" release DIST="$tmp"

amd64="$tmp/$amd64_name"
arm64="$tmp/$arm64_name"
sums="$tmp/SHA256SUMS"

[[ -x $amd64 ]] || fail "missing executable $amd64_name"
[[ -x $arm64 ]] || fail "missing executable $arm64_name"
[[ -f $sums ]] || fail "missing SHA256SUMS"

# Exactly one checksum line per published binary; names are basenames.
mapfile -t sum_lines < <(grep -v '^$' "$sums")
[[ ${#sum_lines[@]} -eq 2 ]] || fail "SHA256SUMS should have exactly 2 lines, got ${#sum_lines[@]}"
printf '%s\n' "${sum_lines[@]}" | grep -q " ${amd64_name}$" || fail "SHA256SUMS missing $amd64_name"
printf '%s\n' "${sum_lines[@]}" | grep -q " ${arm64_name}$" || fail "SHA256SUMS missing $arm64_name"

file "$amd64" | grep -qi 'statically linked' || fail "$amd64_name is not statically linked"
file "$arm64" | grep -qi 'statically linked' || fail "$arm64_name is not statically linked"
file "$amd64" | grep -qi 'x86-64' || fail "$amd64_name is not linux/amd64"
file "$arm64" | grep -Eqi 'ARM aarch64|aarch64' || fail "$arm64_name is not linux/arm64"

(cd "$tmp" && sha256sum -c SHA256SUMS) || fail "SHA256SUMS does not match artifacts"

# Negative: a mutated checksum must not verify.
cp "$sums" "$sums.ok"
first=$(head -c 1 "$sums")
if [[ $first == 0 ]]; then flip=1; else flip=0; fi
{ printf '%s' "$flip"; tail -c +2 "$sums"; } > "$sums.mut"
mv "$sums.mut" "$sums"
if (cd "$tmp" && sha256sum -c SHA256SUMS) >/dev/null 2>&1; then
	fail "expected sha256sum -c to fail after mutating SHA256SUMS"
fi
mv "$sums.ok" "$sums"

# Negative: a missing artifact must not verify.
mv "$amd64" "$amd64.bak"
if (cd "$tmp" && sha256sum -c SHA256SUMS) >/dev/null 2>&1; then
	fail "expected sha256sum -c to fail when $amd64_name is missing"
fi
mv "$amd64.bak" "$amd64"

# Privileged release job must pin every action to a 40-char commit SHA.
assert_action_pins() {
	local wf=$1
	local uses_lines ref line
	uses_lines=$(grep -E '^\s+uses: ' "$wf" || true)
	[[ -n $uses_lines ]] || return 1
	while IFS= read -r line; do
		ref=${line##*@}
		ref=${ref%% *}
		ref=${ref%%#*}
		[[ $ref =~ ^[0-9a-f]{40}$ ]] || return 1
	done <<<"$uses_lines"
	return 0
}

# Tag trigger, make test, make release, and dist/* publication.
assert_workflow_contract() {
	local wf=$1
	grep -q 'tags:' "$wf" || return 1
	grep -q -- '- "v\*"' "$wf" || return 1
	grep -q 'run: make test' "$wf" || return 1
	grep -q 'run: make release' "$wf" || return 1
	grep -q 'files: dist/\*' "$wf" || return 1
	return 0
}

# PR/main CI must test and build; it must not publish.
assert_ci_contract() {
	local wf=$1
	grep -q 'pull_request:' "$wf" || return 1
	grep -q 'run: make test' "$wf" || return 1
	grep -q 'run: make build' "$wf" || return 1
	! grep -q 'action-gh-release' "$wf" || return 1
	return 0
}

assert_action_pins "$workflow" || fail "mutable or missing action pin in $workflow"
assert_workflow_contract "$workflow" || fail "release workflow missing trigger/test/release/dist contract"
assert_action_pins "$ci_workflow" || fail "mutable or missing action pin in $ci_workflow"
assert_ci_contract "$ci_workflow" || fail "ci workflow missing pull_request/test/build contract"

# Prove those assertions reject the mutations QA named.
mut=$(mktemp)
trap 'rm -rf "$tmp" "$mut"' EXIT
for spec in 'v\*' 'make test' 'make release' 'files: dist/\*'; do
	grep -v -E "$spec" "$workflow" >"$mut" || true
	if assert_workflow_contract "$mut"; then
		fail "workflow assertion did not reject mutation of $spec"
	fi
done
for spec in 'pull_request:' 'make test' 'make build'; do
	grep -v -E "$spec" "$ci_workflow" >"$mut" || true
	if assert_ci_contract "$mut"; then
		fail "ci assertion did not reject mutation of $spec"
	fi
done
rm -f "$mut"

# Same-origin SHA256SUMS is a corruption check, not publisher authentication.
# A substituted binary plus a rewritten checksum file still verifies.
echo substituted >"$tmp/agent-usage-linux-amd64"
(cd "$tmp" && sha256sum "$amd64_name" "$arm64_name" >SHA256SUMS)
(cd "$tmp" && sha256sum -c SHA256SUMS) >/dev/null || fail "rewritten same-origin checksums should still verify"
readme="$root/README.md"
grep -q 'does not authenticate the publisher' "$readme" || fail "README must state SHA256SUMS is not publisher authentication"
grep -q 'same GitHub Release' "$readme" || fail "README must state checksum and binary share the release"

echo "release_test: ok"
