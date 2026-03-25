# BitFS v0.0.1 MVP Release + install.sh Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship BitFS v0.0.1 with precompiled binaries and a one-line `curl | sh` installer on bitfs.org.

**Architecture:** Push libbitfs-go to GitHub with tag, update bitfs go.mod to reference remote module (replace → go.work), configure GoReleaser for 7 binaries × 4 platforms, create install.sh that auto-detects OS/arch and downloads from GitHub Releases.

**Tech Stack:** GoReleaser v2, GitHub Releases, shell script (POSIX-compatible)

**Important notes:**
- Tags v0.0.1 may already exist on remotes — delete remote tags before re-pushing.
- Go module proxy may have cached old v0.0.1 — if so, bump to v0.0.2 for libbitfs-go and update all references.
- `websites/` is an independent git repo — commits for install.sh and website changes go there, not in RabbitHole.

---

### Task 0: Pre-flight checks

- [ ] **Step 1: Check if Go module proxy cached old libbitfs-go v0.0.1**

```bash
GONOSUMCHECK=* GOPROXY=https://proxy.golang.org go list -m -json github.com/bitfsorg/libbitfs-go@v0.0.1 2>&1
```

If the proxy returns a result pointing to the OLD commit, we must use `v0.0.2` instead. Note the result and adjust all version references in subsequent tasks accordingly.

- [ ] **Step 2: Check remote tag state**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-go && git ls-remote --tags origin | grep v0.0
cd /Users/alex/Codes/RabbitHole/bitfs && git ls-remote --tags origin | grep v0.0
```

Note which tags exist on remotes. If they exist, Tasks 1 and 6 must delete them first.

---

### Task 1: Push libbitfs-go and tag v0.0.1

**Files:**
- Modify: `/Users/alex/Codes/RabbitHole/libbitfs-go/` (git operations only)

- [ ] **Step 1: Verify tests pass**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./...
```
Expected: All tests PASS

- [ ] **Step 2: Push to GitHub**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-go
git push origin main
```

- [ ] **Step 3: Delete old remote tag (if exists), move tag to HEAD, push**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-go
git push origin :refs/tags/v0.0.1 2>/dev/null || true   # delete remote tag
git tag -d v0.0.1 2>/dev/null || true                    # delete local tag
git tag -a v0.0.1 -m "v0.0.1: initial release"
git push origin v0.0.1
```

If Task 0 determined we need v0.0.2, use `v0.0.2` everywhere instead.

- [ ] **Step 4: Verify Go module proxy indexes the new tag**

```bash
GOPROXY=https://proxy.golang.org go list -m github.com/bitfsorg/libbitfs-go@v0.0.1
```
Expected: `github.com/bitfsorg/libbitfs-go v0.0.1` pointing to HEAD commit. May take up to a minute to propagate.

---

### Task 2: Update bitfs go.mod and create go.work

**Files:**
- Modify: `/Users/alex/Codes/RabbitHole/bitfs/go.mod` (remove replace, update require)
- Modify: `/Users/alex/Codes/RabbitHole/bitfs/go.sum` (auto-updated by go mod tidy)
- Create: `/Users/alex/Codes/RabbitHole/go.work` (local development workspace)
- Modify: `/Users/alex/Codes/RabbitHole/.gitignore` (add go.work*)

- [ ] **Step 1: Remove replace directive from go.mod**

In `bitfs/go.mod`, delete the line:
```
replace github.com/bitfsorg/libbitfs-go => ../libbitfs-go
```

And change the require to reference the tag:
```
require (
    github.com/bitfsorg/libbitfs-go v0.0.1
```

- [ ] **Step 2: Run go mod tidy to update go.sum**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs
GOWORK=off go mod tidy
```

- [ ] **Step 3: Verify build and tests pass WITHOUT workspace**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs
GOWORK=off go build ./cmd/...
GOWORK=off go test ./...
```
Expected: Build succeeds, all tests pass

- [ ] **Step 4: Create go.work for local development**

```bash
cd /Users/alex/Codes/RabbitHole
go work init ./bitfs ./libbitfs-go
```

This auto-detects the correct Go version for the workspace file.

- [ ] **Step 5: Add go.work to .gitignore**

Append to `/Users/alex/Codes/RabbitHole/.gitignore`:
```
go.work
go.work.sum
```

- [ ] **Step 6: Verify local development still works with workspace**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs
go build ./cmd/...
go test ./...
```
Expected: Uses local libbitfs-go via workspace, all passes

- [ ] **Step 7: Commit**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs
git add go.mod go.sum
git commit -m "build: reference remote libbitfs-go v0.0.1, remove replace directive"
```

```bash
cd /Users/alex/Codes/RabbitHole
git add .gitignore
git commit -m "chore: add go.work to gitignore"
```

---

### Task 3: Add GoReleaser configuration

**Files:**
- Create: `/Users/alex/Codes/RabbitHole/bitfs/.goreleaser.yml`

- [ ] **Step 1: Install goreleaser if not present**

```bash
which goreleaser || go install github.com/goreleaser/goreleaser/v2@latest
```

- [ ] **Step 2: Create .goreleaser.yml**

Create `/Users/alex/Codes/RabbitHole/bitfs/.goreleaser.yml` with builds for all 7 binaries (bitfs, bls, bcat, bget, bmget, bstat, btree), targeting darwin/linux × amd64/arm64. Key settings:
- `version: 2`
- `CGO_ENABLED=0` for all builds
- `ldflags: -s -w -X main.Version={{.Version}}` for bitfs only
- `ldflags: -s -w` for b-tools
- Single archive per platform containing all 7 binaries + LICENSE + README.md
- `checksums.txt` with SHA256
- GitHub release to `bitfsorg/bitfs`

Note: The existing Makefile only lists 6 binaries (missing bmget). GoReleaser config should include all 7 from `cmd/`.

- [ ] **Step 3: Validate config**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs
goreleaser check
```
Expected: no errors

- [ ] **Step 4: Test snapshot build**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs
GOWORK=off goreleaser build --snapshot --clean
ls dist/
```
Expected: Binaries for all 4 platforms in dist/

- [ ] **Step 5: Commit**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs
git add .goreleaser.yml
git commit -m "build: add GoReleaser config for cross-platform releases"
```

---

### Task 4: Create install.sh

**Files:**
- Create: `/Users/alex/Codes/RabbitHole/websites/bitfs.org/install.sh`

Note: `websites/` is an independent git repo. Commits go inside that repo.

- [ ] **Step 1: Write install.sh**

The script must:
1. Detect OS (darwin/linux) via `uname -s`
2. Detect arch (amd64/arm64) via `uname -m`
3. Resolve latest version from GitHub API (or accept `BITFS_VERSION` env var)
4. Download `bitfs_{version}_{os}_{arch}.tar.gz` from GitHub Releases
5. Download `checksums.txt` and verify SHA256 (use `shasum -a 256` on macOS, `sha256sum` on Linux)
6. Extract to `BITFS_INSTALL` dir (default: `/usr/local/bin`, fallback `~/.local/bin` if no write permission)
7. Use sudo if needed for `/usr/local/bin`
8. Print success message with version and installed path

Supported env vars:
- `BITFS_VERSION` — pin a specific version (default: latest)
- `BITFS_INSTALL` — override install directory

- [ ] **Step 2: Make executable and test locally**

```bash
chmod +x /Users/alex/Codes/RabbitHole/websites/bitfs.org/install.sh
# Dry-run test: verify platform detection and URL construction
bash -x /Users/alex/Codes/RabbitHole/websites/bitfs.org/install.sh 2>&1 | head -30
```

- [ ] **Step 3: Commit in websites repo**

```bash
cd /Users/alex/Codes/RabbitHole/websites
git add bitfs.org/install.sh
git commit -m "feat: add install.sh for one-line BitFS installation"
```

---

### Task 5: Update bitfs.org install section

**Files:**
- Modify: `/Users/alex/Codes/RabbitHole/websites/bitfs.org/index.html` (install section ~line 1283-1310)

- [ ] **Step 1: Update install section**

Change the CLI window content from the current `go install` command to:
```html
<span class="c-comment"># Install (macOS / Linux)</span>
<span class="c-prompt">$</span> <span class="c-cmd">curl -fsSL</span> <span class="c-path">https://bitfs.org/install.sh</span> <span class="c-cmd">| sh</span>

<span class="c-comment"># Or build from source</span>
<span class="c-prompt">$</span> <span class="c-cmd">go install</span> <span class="c-path">github.com/bitfsorg/bitfs/cmd/...@v0.0.1</span>
```

Note: `cmd/...` installs all 7 binaries (bitfs + b-tools).

Keep the wallet init and hello world lines after it.

- [ ] **Step 2: Commit in websites repo**

```bash
cd /Users/alex/Codes/RabbitHole/websites
git add bitfs.org/index.html
git commit -m "docs: update install section with curl|sh installer"
```

---

### Task 6: Push bitfs and publish GitHub Release

**Files:**
- Modify: `/Users/alex/Codes/RabbitHole/bitfs/` (git operations only)

- [ ] **Step 1: Push bitfs to GitHub**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs
git push origin main
```

This pushes all intermediate commits from Tasks 2, 3, and any prior unpushed work.

- [ ] **Step 2: Delete old remote tag (if exists), move tag to HEAD, push**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs
git push origin :refs/tags/v0.0.1 2>/dev/null || true   # delete remote tag
git tag -d v0.0.1 2>/dev/null || true                    # delete local tag
git tag -a v0.0.1 -m "v0.0.1: initial release"
git push origin v0.0.1
```

- [ ] **Step 3: Run GoReleaser to publish release**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs
GOWORK=off goreleaser release --clean
```
Expected: Builds 7 binaries × 4 platforms, creates 4 archives + checksums.txt, publishes GitHub Release

- [ ] **Step 4: Verify release on GitHub**

```bash
gh release view v0.0.1 --repo bitfsorg/bitfs
```
Expected: Release with 5 assets (4 tar.gz + checksums.txt)

---

### Task 7: Push website and verify end-to-end

- [ ] **Step 1: Push websites repo**

```bash
cd /Users/alex/Codes/RabbitHole/websites
git push origin main
```

Note: Ensure the website deployment pipeline picks up the changes (Cloudflare Pages, manual deploy, etc.).

- [ ] **Step 2: Test install.sh against live release**

```bash
curl -fsSL https://bitfs.org/install.sh | BITFS_INSTALL=/tmp/bitfs-test sh
/tmp/bitfs-test/bitfs --help
```
Expected: Shows bitfs help output with version 0.0.1

If bitfs.org isn't deployed yet, test with the raw file:
```bash
cat /Users/alex/Codes/RabbitHole/websites/bitfs.org/install.sh | BITFS_INSTALL=/tmp/bitfs-test sh
/tmp/bitfs-test/bitfs --help
```
