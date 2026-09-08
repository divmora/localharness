# Security Policy & Responsible Disclosure

The LocalHarness project team takes the security of our runtime engine, protocol, and users seriously.

---

## Supported Versions

We support the current minor version with security patches:

| Version | Supported          |
| ------- | ------------------ |
| `0.1.x` | :white_check_mark: |
| `< 0.1` | :x:                |

---

## Reporting a Vulnerability

If you discover a security vulnerability in LocalHarness, please **do not open a public issue**. Instead, report it privately:

1. **Email**: Send detailed vulnerability information to `security@divmora.com`.
2. **GitHub Security Advisory**: Open a private draft security advisory at [github.com/divmora/localharness/security/advisories/new](https://github.com/divmora/localharness/security/advisories/new).

Please include:
- A description of the vulnerability and its potential impact.
- Steps to reproduce the issue (proof-of-concept harness configuration, prompt, or tool payload).
- Any proposed remediation or patch.

---

## Response Timeline

- **Initial Acknowledgment**: Within 48 hours.
- **Vulnerability Assessment & Triage**: Within 5 business days.
- **Remediation & Advisory Release**: Coordinated with the reporter prior to public disclosure.

---

## Security Best Practices for Users

1. **Workspace Boundary Validation**: Always configure explicit workspace directories. The harness validates tool file operations against configured workspace boundaries.
2. **Interactive Confirmation**: Utilize interactive approval queues (`lhctl` or ADK approval events) when running agents with filesystem mutation or arbitrary command execution capabilities.
3. **API Key & Session Protection**: Pipe handshake tokens and WebSocket API keys should never be logged or exposed to untrusted environments.
