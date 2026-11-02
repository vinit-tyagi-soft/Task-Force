# Security policy

## Supported versions

Security fixes go into the latest release on `main`. Older tags are not maintained separately.

| Version | Supported |
| --- | --- |
| 1.0.x | Yes |
| older | No |

## Reporting a vulnerability

Please report vulnerabilities privately. Do not open a public issue, pull request, or discussion for a security bug.

Use [private vulnerability reporting](https://github.com/vinit-tyagi-soft/Task-Force/security/advisories/new).

Include:

- a description of the issue and the impact
- the version or commit you tested
- steps to reproduce, or a proof of concept that stays inside the report
- whether personal workspace data, another tenant's data, or authentication is affected

The maintainer aims to acknowledge a report within 7 days. This is a single-maintainer project, so a fix can take longer. You will be credited in the advisory if you want to be.

## What to report

- A way to read or change another person's data, including personal workspace data leaking into an organization view, export, webhook, token, alert, or AI answer
- Authentication, session, or CSRF bypasses
- Missing row-level security, overly broad grants, or a migration that weakens an existing policy
- Secret leakage, unsafe webhook delivery, or attachment handling that can execute content
- A workflow or dependency change that exposes repository secrets or publish rights

## Out of scope

- The passwordless demo login (`TF_DEV_AUTH`) when it is used locally as documented. The API refuses to start with it on a non-loopback address or with secure cookies.
- Vulnerabilities in third-party services, or in a deployment that enabled the demo login against the documented rules
- Social engineering of the maintainer, and denial-of-service by sending large amounts of traffic
- Findings from automated scanners that have no demonstrated impact

## Testing rules

Test only against your own local or self-hosted instance. Do not test against a TaskForce server you do not operate, and do not access other people's data.

Give the maintainer time to fix the issue before you publish details.
