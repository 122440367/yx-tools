## Purpose

为不熟悉 yx-tools 参数的用户提供可在 GitHub Pages 直接使用的静态命令生成器，通过勾选和填写表单安全地生成一条可复制执行的 `yx test` 命令。

## ADDED Requirements

### Requirement: Static GitHub Pages operation
The command generator SHALL be deployable as static repository files through GitHub Pages and MUST generate commands entirely in the browser without requiring the yx-tools Go server or another backend. Runtime assets required for core operation MUST be served from the repository rather than third-party CDNs.

#### Scenario: Use from GitHub Pages
- **WHEN** a user opens the published page in a supported browser
- **THEN** the form, validation, command preview, and copy behavior work without contacting a yx-tools API

#### Scenario: Continue after initial load without network
- **WHEN** the page assets have loaded and network access becomes unavailable
- **THEN** changing fields and generating or copying commands continues to work

#### Scenario: Simplified Chinese interface
- **WHEN** the page is displayed
- **THEN** labels, validation messages, instructions, and copy feedback use Simplified Chinese while CLI flags and technical values retain their required English spelling

### Requirement: yx test option coverage
The generator SHALL cover the supported `yx test` executable, speed-test, candidate-source, timeout, output, result-upload, and notification options documented by the CLI. Result upload selection SHALL include no upload plus `api`, `worker`, `github`, and `telegram`. Feishu notification SHALL be a separate setting that can be combined with any upload selection.

#### Scenario: Generate a basic test command
- **WHEN** a user accepts the initial test defaults without selecting upload or notification
- **THEN** the preview is a syntactically executable `yx test` command and contains no upload or notification flags

#### Scenario: Generate upload plus Feishu notification
- **WHEN** a user selects an upload target, enables Feishu notification, and completes both groups
- **THEN** the command contains the selected `-upload` arguments and the independent Feishu notification arguments

#### Scenario: Switch upload modes
- **WHEN** a user changes from one result upload mode to another
- **THEN** fields and flags belonging only to the previous mode are excluded from the generated command while notification settings remain unchanged

#### Scenario: Compact command with advanced fields visible
- **WHEN** a user expands advanced settings but leaves their values at defaults
- **THEN** the generated command continues to omit unchanged default flags

### Requirement: Conditional validation
The generator MUST validate numeric ranges, required conditional fields, enumerated values, and mutually exclusive test choices before presenting a command as ready to copy. Validation feedback SHALL appear next to the affected field and SHALL not rely on color alone.

#### Scenario: Missing upload credential
- **WHEN** a user selects an upload mode but leaves one of that mode's required fields empty
- **THEN** the affected field is identified and the copy action remains unavailable until the input is valid

#### Scenario: Missing Feishu notification field
- **WHEN** Feishu notification is enabled but an App credential or receiver field is empty
- **THEN** the page identifies each missing field and excludes an invalid ready-to-copy state

#### Scenario: Invalid numeric value
- **WHEN** a user enters a value outside the CLI-supported range
- **THEN** the page explains the accepted range and does not mark the command ready

#### Scenario: Correct invalid input
- **WHEN** a user fixes all reported fields
- **THEN** errors clear immediately and the updated command becomes copyable

### Requirement: Platform-aware single-line command rendering
The generator SHALL provide POSIX-shell and PowerShell rendering modes, SHALL render exactly one command line without continuation syntax, and SHALL quote every user-controlled value according to the selected shell. It SHALL offer platform and architecture choices matching documented release artifacts, including Linux, macOS, Windows, and supported architectures, and SHALL derive executable names such as `./yx_linux_arm64`, `./yx_darwin_amd64`, and `.\yx_windows_amd64.exe`. The derived executable path SHALL remain editable and safely quoted.

#### Scenario: Value contains whitespace
- **WHEN** a path or parameter value contains spaces
- **THEN** the generated command preserves the value as one argument in the selected shell

#### Scenario: Value contains quote characters
- **WHEN** a user-controlled value contains shell quote characters
- **THEN** the generated command escapes or quotes the value without creating an additional command or argument

#### Scenario: Switch shell mode
- **WHEN** a user changes the shell selector
- **THEN** the preview is regenerated with the corresponding executable convention and quoting rules

#### Scenario: Select a release platform and architecture
- **WHEN** a user selects Windows amd64, Linux arm64, or another documented release target
- **THEN** the command uses the corresponding published binary filename and shell path convention

#### Scenario: Command remains one line
- **WHEN** the command contains many selected options
- **THEN** the rendered output contains no shell continuation characters or embedded line breaks

### Requirement: Sensitive input privacy
The page MUST NOT send, log, persist, prefill from storage, or place in the URL any form state, including both sensitive and non-sensitive values. Sensitive input fields and their values in the visible command preview SHALL be masked by default. The page SHALL provide an explicit control to reveal sensitive preview values and SHALL warn that copied commands include real secrets which can be retained by shell history and process listings.

#### Scenario: Enter a secret
- **WHEN** a user types an App Secret or token
- **THEN** the value is used only in current in-memory form state, appears masked in the default visible preview, and remains present in the underlying executable command

#### Scenario: Reload the page
- **WHEN** a user reloads or reopens the page
- **THEN** all previously entered or selected form values are absent and the page returns to defaults

#### Scenario: Inspect the URL
- **WHEN** a user changes any sensitive or non-sensitive form value
- **THEN** no form value appears in the page URL or query string

#### Scenario: Reveal sensitive preview values
- **WHEN** a user explicitly enables the reveal control
- **THEN** the preview shows the real sensitive values until the user hides them again or reloads the page

### Requirement: Command preview and copy feedback
The generated command SHALL update as valid inputs change, remain visually distinct and readable on narrow screens, and provide an explicit copy control. Copying SHALL use the real executable command even while its visible preview masks secrets. Copy success or failure SHALL be announced visibly and to assistive technology; if the Clipboard API is unavailable, the user SHALL be able to reveal and select the real command for manual copying.

#### Scenario: Copy succeeds
- **WHEN** a valid command is shown and the Clipboard API accepts the copy request
- **THEN** the real executable command including entered secrets is copied and the page announces success without exposing the secret in the feedback text

#### Scenario: Clipboard access fails
- **WHEN** browser policy rejects clipboard access
- **THEN** the page reports the failure without losing the command and allows manual selection

### Requirement: Responsive and accessible form
The page SHALL support keyboard-only operation, associated visible labels, visible focus states, semantic controls, at least WCAG AA text contrast, and layouts without horizontal page overflow at widths from 375 pixels through desktop sizes. The command preview MAY scroll internally when an unbroken command is wider than its container.

#### Scenario: Keyboard-only use
- **WHEN** a user navigates and operates the generator using only a keyboard
- **THEN** every field, selector, disclosure control, and copy action is reachable in a logical order with a visible focus indicator

#### Scenario: Mobile viewport
- **WHEN** the page is viewed at 375 pixels wide
- **THEN** labels, controls, validation messages, preview, and actions remain readable without overlapping or causing horizontal page scrolling
