## Purpose

为 yx-tools 的测速和结果上传命令提供飞书任务状态通知，使无人值守任务能够向不同类型的飞书用户或群聊报告成功、失败、耗时和汇总信息，而不泄露逐条测速结果。

## ADDED Requirements

### Requirement: Independent Feishu notification mode
The CLI SHALL support explicitly enabling Feishu task notifications with `-notify feishu` independently from result upload selection for both `yx test` and `yx upload`. Notification parameters SHALL include App ID, App Secret, one receive ID, and one receive ID type. Supported receive ID types MUST include `chat_id`, `open_id`, `union_id`, `user_id`, and `email`. Stored Feishu target configuration MUST NOT implicitly enable notifications for later commands.

#### Scenario: Test and upload with notification
- **WHEN** a user runs `yx test` with an existing result upload mode and Feishu notification enabled
- **THEN** the result upload and the Feishu task notification are both attempted using their separate configurations

#### Scenario: Test without result upload
- **WHEN** a user enables Feishu notification without selecting `-upload`
- **THEN** the speed test runs normally and its final execution summary is sent to Feishu

#### Scenario: Notify a standalone upload
- **WHEN** a user enables Feishu notification for `yx upload`
- **THEN** the notification summarizes the standalone upload operation and its outcome

#### Scenario: Reject an unsupported receiver type
- **WHEN** a user selects a receive ID type outside the supported allow-list
- **THEN** the command rejects the notification configuration before starting its primary operation

#### Scenario: Saved target without explicit notification
- **WHEN** non-secret Feishu target configuration exists but a command omits `-notify feishu`
- **THEN** the command performs no Feishu authentication or message request

#### Scenario: One receiver per command
- **WHEN** Feishu notification is enabled
- **THEN** the command sends to exactly one receive ID using exactly one selected receive ID type

### Requirement: Task summary content
The Feishu notification SHALL be plain text and SHALL contain the command operation, final primary-operation status, local-timezone start time, local-timezone end time, human-readable elapsed time, and a concise status message. A test summary SHALL include the number of valid speed-test results. When a result upload was requested, the summary SHALL also include its destination mode, uploaded count when available, and success or failure state. A standalone upload summary SHALL include its destination mode and uploaded result count when available. A displayed failure reason MUST be sanitized and limited to approximately 300 characters.

#### Scenario: Successful speed test
- **WHEN** a speed test completes successfully without a result upload
- **THEN** the notification reports success, elapsed time, and the valid result count

#### Scenario: Successful speed test and upload
- **WHEN** the speed test and configured result upload both succeed
- **THEN** the notification reports the test summary and identifies the successful upload destination

#### Scenario: Result upload fails
- **WHEN** the speed test succeeds but the configured result upload fails
- **THEN** the notification reports the upload destination, failed upload state, elapsed time, and safe failure context

#### Scenario: Speed test fails
- **WHEN** the speed test fails after notification configuration has been validated
- **THEN** the system attempts to send a failure notification before the command exits unsuccessfully

#### Scenario: Local timestamp formatting
- **WHEN** a task summary is formatted
- **THEN** start and end times use the running machine's local timezone in a readable form equivalent to `2026-08-16 21:30:05 CST`, with elapsed time shown separately

### Requirement: Task success and component status semantics
The system MUST treat a completed speed test with zero valid results as a failed primary operation. If speed testing succeeds but result upload fails, the overall primary status MUST be failure while the summary separately reports speed-test success and upload failure.

#### Scenario: Zero valid results
- **WHEN** `yx test` finishes its measurement flow with zero valid results
- **THEN** the notification reports failure with a zero result count and the command exits unsuccessfully

#### Scenario: Upload fails after successful testing
- **WHEN** speed testing and result-file creation succeed but the selected upload destination fails
- **THEN** the summary reports successful testing, failed upload, the upload destination, and an overall failed status

### Requirement: No result-detail disclosure
The Feishu message MUST NOT contain individual IP addresses, ports, per-IP delays, per-IP speeds, loss rates, access tokens, App Secret values, or other uploaded file contents. Failure text included in the summary MUST be sanitized so credentials are not exposed.

#### Scenario: Successful test has many results
- **WHEN** a completed speed test contains one or more result rows
- **THEN** the Feishu message contains only the aggregate result count and no row-level values

#### Scenario: Error includes a remote response
- **WHEN** an upstream API error is included in the task summary
- **THEN** the notification keeps useful safe context while redacting configured secrets and bearer tokens

### Requirement: Feishu application authentication and delivery
The system SHALL exchange the configured App ID and App Secret for a tenant access token through the Feishu internal application authentication API and SHALL use the token as a bearer credential to send the summary through the Feishu message API using the selected receive ID type and receive ID. Non-successful HTTP responses, malformed success responses, non-zero Feishu platform codes, and context cancellation MUST be treated as notification failures.

#### Scenario: Successful notification delivery
- **WHEN** Feishu accepts the credentials and message request
- **THEN** the summary is delivered to the selected receiver and the command reports no notification error

#### Scenario: Authentication fails
- **WHEN** Feishu rejects the App ID or App Secret
- **THEN** no message request is attempted and the notification failure is reported without exposing the credentials

#### Scenario: Message API reports an error
- **WHEN** Feishu returns a non-zero platform code or unsuccessful HTTP status for the message request
- **THEN** the command reports a contextual notification failure including safe platform status information

### Requirement: Bounded retry and duplicate prevention
The notification client SHALL use a total retry budget of 10 seconds and SHALL make at most two retry attempts after the initial request. It SHALL honor a valid `Retry-After` value, otherwise using short delays equivalent to approximately 500 milliseconds and 1 second. Retries MUST reuse a stable idempotency identifier when supported. If duplicate prevention cannot be guaranteed, the client MUST NOT retry an ambiguous message-send timeout that might already have been accepted.

#### Scenario: Explicit rate limit response
- **WHEN** Feishu returns HTTP 429 with a usable `Retry-After` value and the retry budget remains
- **THEN** the client waits accordingly and retries without exceeding two retries or the total budget

#### Scenario: Temporary server error
- **WHEN** Feishu returns a retryable 5xx response
- **THEN** the client retries with bounded backoff and a stable idempotency identifier

#### Scenario: Ambiguous message timeout without idempotency
- **WHEN** a message request times out after it might have reached Feishu and the API cannot guarantee idempotency
- **THEN** the client returns a notification error without retrying that ambiguous send

### Requirement: Cancellation notification
When an enabled `yx test` or `yx upload` operation is cancelled by the user, the system SHALL attempt one final plain-text `cancelled` summary using a new context independent of the cancelled task context and bounded to 5 seconds. The command MUST still exit unsuccessfully after the attempt.

#### Scenario: User interrupts a running test
- **WHEN** the user sends an interrupt after valid Feishu notification preflight
- **THEN** the primary work is cancelled and the system attempts a cancellation notification for no longer than 5 seconds

#### Scenario: Cancellation notification also times out
- **WHEN** the independent cancellation notification reaches its 5-second deadline
- **THEN** the command stops waiting, reports the notification failure, and exits unsuccessfully

### Requirement: Primary outcome preservation
The system SHALL calculate the task status before attempting the final notification. Notification failure MUST NOT rewrite a failed primary operation as successful or change a successful primary operation's summary text to claim the primary work failed. If a requested notification cannot be delivered, the overall command MUST still exit unsuccessfully after reporting the notification error.

#### Scenario: Primary task fails and notification succeeds
- **WHEN** the primary test or upload fails and the failure notification is delivered
- **THEN** the command exits unsuccessfully with the primary failure

#### Scenario: Primary task succeeds and notification fails
- **WHEN** the primary work succeeds but Feishu delivery fails
- **THEN** the command reports that the primary work succeeded, reports the separate notification failure, and exits unsuccessfully

#### Scenario: Both primary task and notification fail
- **WHEN** the primary work fails and Feishu delivery also fails
- **THEN** the command preserves both errors for diagnostics and exits unsuccessfully

### Requirement: Non-secret target persistence and App Secret non-persistence
After a successfully delivered Feishu notification, the system SHALL persist the App ID, receive ID, and receive ID type in the existing protected local configuration. It MUST NOT persist the App Secret. Every notification-enabled command MUST receive a non-empty App Secret from the current CLI invocation; non-empty CLI values for the other Feishu fields SHALL take precedence over stored values.

#### Scenario: Reuse saved non-secret target configuration
- **WHEN** a prior notification succeeded and a later command enables Feishu notification with an App Secret but omits the saved App ID or receiver fields
- **THEN** the later command uses the stored non-secret target configuration

#### Scenario: Override a saved value
- **WHEN** a command supplies a non-empty Feishu parameter different from the stored value
- **THEN** the supplied non-secret value is used and is persisted only after successful notification delivery

#### Scenario: App Secret is never saved
- **WHEN** a Feishu notification succeeds with a supplied App Secret
- **THEN** neither `yx-config.json` nor any other project-managed persistent storage contains that App Secret

#### Scenario: Effective configuration is incomplete
- **WHEN** the App Secret is absent from the current CLI invocation or another required field is absent from both CLI input and stored non-secret configuration
- **THEN** the primary operation does not start and the CLI identifies the missing fields without printing secret values
