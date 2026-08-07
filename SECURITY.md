# Security Policy

## Reporting a vulnerability

Please **do not** open a public issue for security vulnerabilities. Report them
privately through GitHub's
[private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/working-with-security-advisories/configuring-private-vulnerability-reporting-for-a-repository)
for this repository.

When reporting, include:

- A description of the vulnerability and its impact
- The affected version(s) and, if known, the commit(s) that introduced it
- Steps to reproduce, or a minimal proof of concept
- Any suggested fix, if you have one

You should receive a response acknowledging the report within a few business
days. We will work with you to understand and address the issue, and will
coordinate disclosure once a fix is available.

## Scope

This policy covers the `eidos` CLI and its generated output. Note that eidos
**generates** Terraform provider code from OpenAPI specs — a spec is untrusted
input. The parser and transformer are hardened against malformed or hostile
specs (fail-loud diagnostics, no silent drops), and remote `--spec` URL fetching
applies an SSRF guard, size/timeout caps, and scheme allowlisting. If you find a
way to bypass those protections, please report it.

## Supported versions

Security fixes are applied to the latest release. We do not maintain a
long-term-support branch; please upgrade to the newest release to receive fixes.
