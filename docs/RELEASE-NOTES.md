# FlipAi v0.46.31

Security-hardening release focused on normal Windows behavior, safer updates, and provider isolation without changing FlipAi's everyday workflow.

## Windows and endpoint-security hardening

- Normal Start with Windows now uses the Windows registry API instead of spawning a hidden registry command.
- Optional pre-sign-in startup remains user-controlled and its long-running task runs at least privilege.
- Removed the installer's hidden `taskkill.exe` fallback; updates/uninstalls use FlipAi's bounded graceful shutdown path.
- Claude Code setup uses Anthropic's WinGet package instead of downloading and executing a PowerShell install script.
- Added normal Windows EXE metadata, icon resources, and an `asInvoker` manifest; release binaries are no longer stripped with Go `-s -w` flags.
- Hardened update downloads to trusted GitHub HTTPS endpoints, mandatory SHA-256 verification, private per-user staging, filename/size checks, redirect restrictions, and atomic writes.

## Provider/filter isolation

- ChatGPT Chat, Claude Chat, Gemini Chat, and Grok Chat remain independent provider workers with separate WebView profiles, state, and authenticated loopback control endpoints.
- If a content filter or DNS/proxy policy blocks one provider, that provider is reported unavailable without shutting down FlipAi or unrelated agents.
- Added regression tests to prevent a provider-specific browser failure from becoming an app-wide shutdown.

## Release security

- Added CodeQL and `govulncheck` security scanning, Dependabot configuration, SBOM generation, and GitHub build-provenance attestations.
- Defender definitions are refreshed before release scans; CI scans the compiled EXE, installer, and installed program image.
- Added regression guards against process-injection primitives, antivirus-disabling commands, encoded/remote script execution, shell persistence regressions, elevated long-running startup, and forced hidden process-tree killing.
- Documented the complete 30-point hardening audit in `docs/SECURITY-HARDENING.md`.

No Authenticode/code-signing certificate is included in this release.
