# Security Audit Command

Perform a comprehensive security audit of the logwatch-ai-go codebase.

## What This Command Does

Launches a thorough security analysis covering:

1. **Code Security**
   - Credential exposure (API keys, tokens in code/logs)
   - SQL injection vulnerabilities (SQLite queries)
   - Command injection (Bash execution, file paths)
   - Path traversal (file operations)
   - Prompt injection protection (Claude AI input sanitization)

2. **Dependency Security**
   - Known CVEs in Go modules (go.mod)
   - Outdated dependencies with security patches
   - Vendor code vulnerabilities
   - Transitive dependency risks

3. **Configuration Security**
   - .env file handling and permissions
   - API credential validation patterns
   - Proxy configuration security
   - Database connection security

4. **Data Security**
   - Log sanitization (internal/errors, internal/logging)
   - Sensitive data in database
   - Telegram message content exposure
   - Historical context privacy

5. **Deployment Security**
   - File permissions in /opt/logwatch-ai
   - Cron job security
   - Log file access control
   - Network security (HTTPS proxy)

## Critical Security Features to Verify

This project implements several security measures that should be audited:

- **Credential Sanitization**: `internal/errors/sanitizer.go` redacts API keys and tokens
- **Secure Logging**: `internal/logging/logger.go` filters credentials from logs
- **Prompt Injection Protection**: `internal/ai/prompt.go` sanitizes logwatch content
- **Input Validation**: `internal/config/config.go` validates all configuration

## Use Cases

- **Pre-release audit**: Before deploying new version to production
- **Post-dependency update**: After adding/updating Go modules
- **Security review**: Periodic security assessment
- **Incident investigation**: After suspected security issue

## Expected Output

The security audit will generate:
- List of vulnerabilities found (categorized by severity)
- Specific file locations and line numbers
- Remediation recommendations
- Compliance with security best practices
- Summary report with risk assessment

## Follow-up Actions

After the audit:
1. Review findings with security team
2. Prioritize fixes by severity
3. Update `.audit/` directory with results (never commit)
4. Create tickets for remediation
5. Re-run audit after fixes
