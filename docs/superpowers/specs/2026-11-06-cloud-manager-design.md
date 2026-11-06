# YunTools approved design

The user approved this design and implementation on 2026-11-06.

Build a Go + MyGo native desktop cloud manager, with the same application/platform/view separation and visual system as SSHYard. Independent implementation; do not copy SSHYard product code. MyGo v0.7.4, Go 1.27.3. Chinese UI initially, light/dark themes, blue #1984FC accent, 4 DIP spacing grid, unified title bar, resource sidebar, tabs, detail panels and persistent task center.

Support multiple encrypted cloud profiles, initially Aliyun, through capability-specific provider interfaces. Never put credentials or certificate keys in plaintext configuration/logs. A system keychain stores a random 32-byte master key; an authenticated encrypted atomic file stores profiles and sensitive job metadata. Failure to unlock must not fall back to plaintext. Accounts and tasks stay isolated across account switches.

OSS: buckets and object CRUD, pagination and prefix navigation, folder upload/download, metadata, copying/moving, explicit confirmation before every deletion (including the source deletion in a move). Batch deletion uses the exact confirmed manifest. Do not delete historical versions implicitly. Upload and download support persisted checkpoints, restart recovery, pause/resume, source identity checks, real progress and failures. Local paths derived from object names must never escape the selected destination.

Whitelist: RDS, Redis, ECS security groups. List targets/rules, detect public IP, manually add or enable periodic automatic append for explicitly selected targets. Preserve other rules and old IPs. Security groups require explicit protocol and port bounds. Dedupe, validate, append, verify; show per-resource outcomes.

DNS: list domains, add zones, list/query/add records only. No general update, delete or disable method in the public DNS capability. ACME cleanup exception explicitly approved: delete only a challenge TXT created by this application, identified by persisted record ID, exact name and value, and re-read before removal. Never delete any unrelated DNS record.

Certificates: lego ACME DNS-01, explicit first-time email/terms agreement, regular and wildcard domains, propagation status, encrypted account/certificate private keys, certificate export only on explicit user action, manual renewal and optional scheduled renewal while app is running/unlocked. No automatic deployment. Staging mode for tests. Persistent ownership journal for TXT cleanup and recovery.

Build and test without user's real cloud credentials. Use local HTTP fixtures and ui.Tester; cloud acceptance testing needs the user's test account, never infer authorization to change real resources during development.
