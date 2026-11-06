# YunTools Implementation Plan

> **For agentic workers:** use superpowers:executing-plans and dispatch independent capability modules with superpowers:dispatching-parallel-agents. Track steps here.

**Goal:** Deliver a runnable native multi-account cloud manager with OSS, whitelist, restricted DNS and ACME certificates.

**Architecture:** MyGo App/View with injected Platform. Capability-specific cloud interfaces, encrypted KV storage, persistent cancellable background jobs. Provider clients bind immutable account snapshots.

**Tech Stack:** Go 1.27.3, MyGo v0.7.4, Alibaba OSS Go SDK V2, Alibaba RPC SDK, lego ACME, AES-GCM.

**Spec:** ../specs/2026-11-06-cloud-manager-design.md

## Global Constraints
- Native Go UI only; #1984FC accent, light/dark themes.
- No plaintext accounts, tokens, ACME private keys or credentials in logs.
- Ordinary DNS has no mutation except Add; certificate cleanup requires owned exact TXT match.
- Explicit second confirmation before cloud deletion. No real cloud mutation during development.
- Tasks bind account IDs; switching accounts never changes task identity.

## Review Focus
- Credentials rotated/deleted while a task is paused: require the correct current profile on resume.
- Cloud response arrives after account switch: discard stale resource results.
- Crash between creating TXT and saving ownership: retain unknown records, never guess ownership.
- Malicious object key/local source mutation: prevent traversal and mixed-content downloads.
- Timeout after a create request: query before retrying non-idempotent changes.

### Task 1: Contracts and encrypted persistence
Files: internal/cloud/types.go, internal/keychain/*.go, internal/accounts/*.go.
Interfaces: cloud.SecretStore Get/Put/Delete; keychain.Open(dir, Backend) returns Store; Backend Get/Set/Delete with ErrNotFound.
- [x] Write/run failing tests for wrong keys, tampering, round trip, no plaintext, concurrent writes and profile isolation.
- [x] Implement atomic AES-GCM KV store and OS keychain adapter; account CRUD via encrypted KV.
- [x] Run go test -race ./internal/keychain ./internal/accounts.

### Task 2: OSS provider
Files: internal/providers/aliyunoss/*.go, internal/storage/*.go.
Interfaces: cloud.ObjectStorage in internal/cloud/types.go; New(cloud.Account) factory.
- [x] Write/run failing tests for confirmed delete, source mutation, object path traversal, upload/download checkpoint options.
- [x] Implement official SDK provider: buckets, listing, stat, metadata/copy/delete, transfers and progress.
- [x] Run go test -race ./internal/providers/aliyunoss ./internal/storage.

### Task 3: DNS, whitelist and certificates
Files: internal/providers/aliyun/*.go, internal/certificates/*.go.
Interfaces: cloud.DNS, cloud.Whitelist, cloud.ChallengeDNS; certificates service uses cloud.SecretStore.
- [x] Write/run failing tests for DNS additive-only surface, deduped whitelist append, exact TXT ownership checks.
- [x] Implement RPC provider for DNS/RDS/Redis/ECS and public IP detection.
- [x] Implement lego DNS-01 with encrypted key storage, progress, owned cleanup and renewal/export.
- [x] Run go test -race ./internal/providers/aliyun ./internal/certificates.

### Task 4: Persistent background tasks and application
Files: internal/tasks/*.go, internal/app/*.go, internal/theme/*.go, main.go.
Interfaces: tasks.Manager Submit/Pause/Resume/Snapshot; App.View(*ui.Context), Platform.
- [x] Write/run failing tests for pause/restart, account isolation, stale response rejection and delete cancel.
- [x] Implement native UI: account manager, OSS, whitelist, DNS, certificate pages, task center, settings.
- [x] Bind all visible actions to real providers; no fake success or invented resources.
- [x] Add ui.Tester interaction tests and render/run visual validation.

### Task 5: Verification and delivery
Files: README.md, scripts/check.sh, mygo.json, resources/icon.png.
- [x] Run gofmt, focused tests, then full go test -race ./... and go vet ./....
- [x] Build using MyGo build; inspect light/dark renders at 1280×820 and launch the native macOS UI.
- [x] Independent review of encryption, confirmation and DNS cleanup boundaries; fix actionable findings.
- [x] Document run/build commands, permissions, limitations and verification evidence.

## Execution notes
- Working in the user's empty workspace; no existing Git branch/worktree to isolate. No reapproval needed: user explicitly approved implementation.
- Dependency downloads use a temporary GOPATH and cache to keep the workspace clean.

## Final verification — 2026-11-06
- `scripts/check.sh` passed: formatting, `go test -race -count=1 ./...`, and `go vet ./...`.
- `TestVisualScreens` passed; overview, OSS, DNS and account form render in light/dark at 1280×820. Overview and account form images were inspected.
- MyGo build passed for darwin/arm64, windows/amd64 and linux/amd64. macOS native app startup and visible layout were checked; Windows/Linux runtime checks remain outstanding.
- Independent review findings addressed: immutable form input, account edit invalidation, multipart copy identity conditions, download destination anchoring, certificate bytes persistence, and non-blocking renewal scheduling.
- No real cloud mutation or real ACME issuance was performed by the implementation/testing workflow. Cloud integration requires the user's test account and resources.
- Current workspace has no Git repository; no commit or PR was created.
