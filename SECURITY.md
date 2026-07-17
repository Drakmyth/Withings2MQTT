# Security Policy

## Supported versions

Withings2MQTT is under active development. Security fixes are applied to the latest revision of the default branch; older revisions are not supported.

## Reporting a vulnerability

Please do not open a public issue for a suspected vulnerability or include OAuth tokens, encryption keys, health data, database files, or other sensitive information in an issue or discussion.

Use GitHub's private vulnerability reporting feature from the repository's **Security** tab. Include the affected revision, impact, reproduction steps, and any suggested mitigation. If private vulnerability reporting is unavailable, contact the repository owner privately before disclosing details.

Reports will be acknowledged as soon as practical. Confirmed issues will be assessed, fixed on the default branch, and disclosed after a remediation is available.

## Deployment boundary

The admin interface is designed for localhost access. Do not expose it to an untrusted network without adding authentication, HTTPS, and appropriate network controls. Treat the SQLite database, OAuth credentials and tokens, and `WITHINGS2MQTT_TOKEN_ENCRYPTION_KEY` as sensitive.
