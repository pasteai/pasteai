# Security Policy

## Reporting a Vulnerability

Please report security vulnerabilities via **GitHub's private advisory system**:
[github.com/pasteai/pasteai/security/advisories/new](https://github.com/pasteai/pasteai/security/advisories/new)

Do not open a public GitHub issue for security vulnerabilities — that exposes users before a fix is available.

We aim to acknowledge reports within 48 hours and resolve confirmed issues within 30 days.

## Self-Hosted Deployments

- Always set `-api-key` if the server is accessible to anyone other than yourself.
- Set `-base-url` for any non-localhost deployment to prevent host-header injection in generated links.
- Do not expose port 8080 directly to the internet; use a reverse proxy with TLS.

## Scope

Self-hosted instances are the operator's responsibility. Issues in the PasteAI binary, API, or MCP server are in scope.
