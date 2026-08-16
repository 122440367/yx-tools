## 1. Feishu notification foundation

- [x] 1.1 Extend `Config` with Feishu App ID, receive ID, and receive ID type only; never add or serialize App Secret, default an omitted type to `chat_id`, and add backward-compatible `0600` load/save tests.
- [x] 1.2 Add `FeishuTarget` and `TaskSummary` plus single-receiver validation and a deterministic plain-text formatter containing local start/end times, human duration, counts, upload component state, and a sanitized failure reason limited to about 300 characters while excluding all IP-level details.
- [x] 1.3 Implement tenant token exchange and text-message delivery with context-aware standard-library HTTP calls, safe platform response validation, and replaceable endpoints/client for `httptest`.
- [x] 1.4 Implement a 10-second total retry budget, at most two retries, `Retry-After`, approximately 500ms/1s fallback delays, and stable idempotency identifiers; do not retry ambiguous sends when duplicate prevention cannot be guaranteed.
- [x] 1.5 Add table-driven `httptest` coverage for successful auth/send, every supported receive ID type, invalid type, one-receiver enforcement, 429/5xx retry, retry limits, idempotency reuse, ambiguous timeout, malformed/platform errors, cancellation, and credential redaction.

## 2. CLI lifecycle and notification integration

- [x] 2.1 Add shared flags for explicit `-notify feishu`, `-feishu-app-id`, `-feishu-app-secret`, `-feishu-receive-id`, and `-feishu-receive-id-type` to `test` and `upload`; require App Secret from every enabled invocation while allowing only non-secret fields to use config fallback.
- [x] 2.2 Refactor `doUpload` to return `uploadOutcome` and error rather than calling `os.Exit`, then update `test`, `upload`, and `proxy` while preserving existing upload output and persistence.
- [x] 2.3 Add a unified `runTest` outcome path with local start/end times and elapsed time; treat zero valid results as failure and preserve separate test, file-write, and upload component states.
- [x] 2.4 Apply the same outcome/finalization model to standalone `runUpload`, including input-read failures, upload target/status/count, local timestamps, and safe failure summaries.
- [x] 2.5 Attempt Feishu success/failure notification at the single finalization point, return non-zero for requested notification failure, and use `errors.Join` without duplicate logging.
- [x] 2.6 On Ctrl+C, cancel primary work and attempt a plain-text cancellation summary through a separate 5-second context before returning the cancellation error.
- [x] 2.7 Persist only successful non-secret Feishu target fields, prove App Secret never reaches `yx-config.json`, and ensure saved target data never implicitly enables notification.
- [x] 2.8 Add CLI tests for explicit opt-in, no implicit notification, zero results, notification failure, combined errors, local timestamps, and absence of IP/secret values.

## 3. Static yx test command generator

- [x] 3.1 Create Simplified-Chinese `docs/index.html`, `docs/style.css`, and `.nojekyll` with a responsive single-column form, semantic groups, progressive advanced fields, masked secret inputs/preview, reveal control, copy action, and shell-history/process-list warning.
- [x] 3.2 Implement DOM-independent `docs/generator.mjs` covering every supported `yx test` flag, compact omission of unchanged defaults, stable argv order, upload conditional fields, independent Feishu notification fields, and structured validation.
- [x] 3.3 Add documented release mappings for Windows amd64/arm64, Linux amd64/arm64/386, macOS amd64/arm64, and FreeBSD amd64, producing paths such as `.\\yx_windows_amd64.exe`, `./yx_linux_arm64`, and `./yx_darwin_amd64`, while allowing manual executable overrides.
- [x] 3.4 Implement POSIX and PowerShell quoting that safely renders exactly one line and preserves whitespace and quote characters in executable paths, files, URLs, IDs, tokens, and secrets.
- [x] 3.5 Implement `docs/app.mjs` with transient in-memory form state, conditional sections, adjacent accessible validation, separate masked-display and real-copy commands, invalid-copy disabling, reveal/manual-copy fallback, and `aria-live` feedback that never echoes secrets.
- [x] 3.6 Ensure the page contains no backend calls, analytics, CDN assets, remote fonts, storage APIs, URL serialization, or persistence of sensitive or non-sensitive state; reload must restore defaults.
- [x] 3.7 Add zero-dependency `node:test` cases for compact defaults, every upload mode, upload plus Feishu, platform/architecture names, editable executable paths, single-line output, validation boundaries, mode switching, masking versus real copy, shell quoting, and non-persistence.

## 4. Documentation and verification

- [x] 4.1 Update CLI usage/help and README with explicit notification opt-in, receive ID types, one-target plain-text behavior, message fields, zero-result and partial-upload failure semantics, cancellation notification, retry/idempotency limits, required Feishu permissions, and App Secret non-persistence.
- [x] 4.2 Document shell-history/process-list risks, masked preview versus real-value copy, supported platform/architecture binary names, compact single-line generation, `/docs` GitHub Pages publishing, and JavaScript test commands.
- [ ] 4.3 Run `gofmt`, `go test ./...`, `go test -race ./...`, `node --test docs/generator.test.mjs`, `git diff --check`, and strict OpenSpec validation; resolve every failure.
- [ ] 4.4 Manually verify the Simplified-Chinese page at 375px and desktop widths, keyboard-only navigation, focus visibility, platform/architecture switching, conditional fields, masked/revealed preview, real-value copy/fallback, one-line internal scrolling, and absence of all form state from URL/storage.
