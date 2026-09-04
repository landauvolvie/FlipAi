# FlipAi Windows security-hardening checklist

This file closes the security/endpoint-protection hardening pass without adding Authenticode signing. The goal is not to evade security software; it is to make FlipAi use ordinary, least-privilege Windows mechanisms and keep risky capabilities narrow and auditable.

| # | Check | Status |
|---|---|---|
| 1 | Main FlipAi process stays non-admin | Done. Normal app/host/tray/browser processes run as the user. UAC is limited to the explicit pre-sign-in task toggle. |
| 2 | Remove unnecessary PowerShell/CMD subprocesses | Done where non-interactive work was involved. Remote PowerShell install was removed. The visible Claude sign-in console remains because it is an interactive user flow. |
| 3 | Avoid shell-style persistence | Done for normal startup: HKCU Run uses the Windows registry API. The optional pre-sign-in feature retains Windows `schtasks.exe` only for its fixed FlipAi task because Task Scheduler is the feature being requested; it is opt-in, fixed-name, fixed-command, and least-privilege. |
| 4 | Startup persistence must be opt-in | Done. Pre-sign-in startup is created only after the user enables it. |
| 5 | Avoid elevated long-running tasks | Done. The boot task uses `LeastPrivilege`; `HighestAvailable` is prohibited by regression tests. |
| 6 | Isolate privileged operations | Done. The only UAC path accepts fixed `install`/`remove` boot-task actions and exits immediately. A second unsigned helper EXE was deliberately not added because that would expand the binary/reputation surface without adding capability isolation. |
| 7 | Do not generate and execute scripts/binaries from TEMP | Done for executable/script paths. Update EXEs stage under FlipAi's private per-user data directory. The boot feature may create a temporary XML task definition only; it is data, not executable code. |
| 8 | Do not patch other applications | Done. Provider inspection/automation is read-only or uses each provider's dedicated WebView/page controls; FlipAi does not rewrite installed ChatGPT/Claude/Gemini/Grok binaries. |
| 9 | Scope browser automation | Done. Each web provider has its own dedicated WebView/profile and in-process page driver. Debug mode is off. |
| 10 | Lock down local control endpoints | Done. Provider control servers bind to `127.0.0.1:0` and require a cryptographically random `X-FlipAi-Token`. No remote-debugging port is exposed. |
| 11 | Protect credentials | Done/already present. Secrets use FlipAi's protected secret storage and sensitive browser sessions stay in provider-specific per-user profiles. |
| 12 | Keep secrets/message bodies out of diagnostics | Done/already present. Exported diagnostics are statuses/logs; security tests and filtering avoid deliberate credential/token logging. |
| 13 | Use direct process execution with structured arguments | Done for background work. Shell command composition is not used for agent prompts or remote content. Interactive Claude sign-in is the narrow visible-console exception. |
| 14 | Restrict executable discovery | Done with a compatibility exception: automatic Codex/Claude discovery uses expected install locations/PATH; an explicit path entered by the user remains allowed. |
| 15 | Clean up child processes safely | Done through supervised processes, contexts, authenticated stop endpoints, and bounded graceful shutdown. Windows Job Objects were not added because some launched agent/browser processes have provider-owned child trees and forcibly owning them could change user-visible behavior. |
| 16 | Verify downloads before execution | Done. Updates are HTTPS/GitHub-only, size-limited, SHA-256 manifest verified, re-hashed when reused, and atomically staged. |
| 17 | Do not execute updates from Downloads/TEMP | Done. Updates execute only from FlipAi's private per-user `updates` directory. |
| 18 | Keep updater capability narrow | Done without adding another EXE. Update logic is isolated in `update.go`, only GitHub release endpoints are accepted, and the existing Inno installer performs the replacement. A second unsigned updater executable was deliberately avoided. |
| 19 | Embed normal Windows metadata | Done. Product/file/version/original-filename metadata and the FlipAi icon are embedded in the EXE. |
| 20 | Embed a least-privilege Windows manifest | Done. The main EXE is `asInvoker`; regression tests prohibit an admin manifest. |
| 21 | Keep analyzable Go metadata | Done. Release/build commands no longer strip with `-s -w`. |
| 22 | No packing/obfuscation | Done. No UPX/runtime unpacker/obfuscator is used. |
| 23 | Use private per-user storage | Done/already present. Sensitive files and update staging live under the user's FlipAi data directory with restrictive file modes/inherited per-user Windows ACLs. |
| 24 | Validate attachments/files | Done/already present. Attachment handling has size/name/path controls and does not automatically execute received content. |
| 25 | HTTPS and normal certificate validation | Done. Remote update/provider traffic uses HTTPS; no TLS-validation bypass is introduced. |
| 26 | Automated source/dependency security scanning | Done. CodeQL and `govulncheck` run in CI; Dependabot configuration remains present. GitHub Dependency Review was removed because this repository does not have Dependency Graph enabled. |
| 27 | Harden GitHub Actions | Done for this pass. Workflows use minimum practical token permissions; release write/OIDC permissions are limited to release provenance/publishing. Third-party release behavior is kept in repository workflows rather than downloaded scripts. |
| 28 | Defender-gate the real Windows artifacts | Done. CI refreshes Defender intelligence and scans the raw EXE, installer, and installed application image before release. |
| 29 | Publish provenance and SBOM | Done. Release artifacts receive GitHub build-provenance attestations and releases generate/publish an SBOM. |
| 30 | Prevent malware-style regressions | Done. Static/regression tests reject process injection primitives, antivirus-disabling commands, encoded/remote script execution, shell persistence regressions, elevated long-running startup, hidden forced process-tree killing, and loss of provider isolation. |

## Content-filter isolation

ChatGPT Chat, Claude Chat, Gemini Chat, and Grok Chat run as separate provider workers with separate profiles, state and authenticated loopback control endpoints. A provider-specific DNS/proxy/content-filter failure is reported as that agent being unavailable and does not request a FlipAi-wide shutdown. Codex and Claude Code remain separate local-agent paths as well. Blocking one provider must therefore not take down the application or unrelated providers.
