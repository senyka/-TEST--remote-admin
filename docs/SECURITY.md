# Security Checklist for RemAdm

## 🔐 Mandatory Security Measures

### 1. Transport Layer Security

- [ ] **TLS 1.3** for all HTTPS/WSS connections
- [ ] **DTLS-SRTP** for WebRTC media encryption
- [ ] Valid certificates from trusted CA (not self-signed in production)
- [ ] HSTS headers enabled
- [ ] Certificate pinning for agent connections

### 2. Authentication & Authorization

- [ ] **Argon2id** password hashing (minimum: 64MB memory, 3 iterations, 4 parallelism)
- [ ] **TOTP 2FA** support (RFC 6238)
- [ ] JWT tokens with short TTL (15 minutes) + refresh tokens
- [ ] Session confirmation via push/email before granting access
- [ ] Device registration requires user approval
- [ ] Rate limiting on authentication endpoints

### 3. WebRTC Security

- [ ] Mandatory DTLS handshake verification
- [ ] SRTP fingerprint validation in SDP
- [ ] ICE candidate filtering (prevent SSRF)
- [ ] TURN server with credential mechanism (RFC 5389)
- [ ] DataChannel encryption enabled
- [ ] Media stream isolation between sessions

### 4. Agent Security

- [ ] Binary signing with **cosign/sigstore**
- [ ] Run as non-privileged user
- [ ] **SELinux/AppArmor** profiles enforced
- [ ] Minimal capabilities (no root unless required)
- [ ] Secure update mechanism with signature verification
- [ ] Memory-safe code (Rust helps here)
- [ ] Input validation on all received commands

### 5. Database Security

- [ ] PostgreSQL with SSL mode required
- [ ] Parameterized queries (prevent SQL injection)
- [ ] Least privilege database users
- [ ] Encrypted connections to Redis
- [ ] Session data encrypted at rest
- [ ] Regular backup encryption

### 6. Audit & Logging

- [ ] All session initiations logged (who, when, which device)
- [ ] Failed authentication attempts logged
- [ ] Session recording (optional, with user consent)
- [ ] Log integrity protection (write-once storage)
- [ ] Real-time alerting for suspicious activity
- [ ] GDPR-compliant log retention policies

### 7. Frontend Security

- [ ] Content Security Policy (CSP) headers
- [ ] XSS protection via React's built-in escaping
- [ ] CSRF tokens for state-changing operations
- [ ] Secure cookie flags (HttpOnly, Secure, SameSite)
- [ ] Subresource Integrity (SRI) for CDN resources
- [ ] Regular dependency vulnerability scanning

### 8. Network Security

- [ ] Firewall rules limiting port access
- [ ] DDoS protection (rate limiting, Cloudflare, etc.)
- [ ] TURN server behind NAT/firewall
- [ ] No direct P2P if behind symmetric NAT (force TURN)
- [ ] Network segmentation for database

### 9. Operational Security

- [ ] Regular security audits (quarterly)
- [ ] Penetration testing (annual)
- [ ] Dependency updates (automated via Dependabot/Renovate)
- [ ] Incident response plan documented
- [ ] Disaster recovery procedures tested
- [ ] Security monitoring (SIEM integration)

## 🚨 Critical Vulnerabilities to Avoid

1. **Never** use plain HTTP/WebSocket in production
2. **Never** store passwords in plaintext or weak hashes (MD5, SHA1, bcrypt with low cost)
3. **Never** allow unauthenticated WebSocket connections
4. **Never** skip certificate validation in agent
5. **Never** run agent as root/system without necessity
6. **Never** log sensitive data (passwords, tokens, PII)
7. **Never** use default credentials for databases
8. **Never** expose database ports to public internet

## ✅ Pre-Launch Security Review

Before deploying to production:

- [ ] Complete OWASP Top 10 review
- [ ] Third-party security audit
- [ ] Load testing with security monitoring
- [ ] Backup/restore test
- [ ] Failover and disaster recovery test
- [ ] Documentation of all security controls
- [ ] User privacy policy updated
- [ ] Compliance check (GDPR, SOC2, etc. if applicable)

## 📚 References

- [OWASP Web Security Testing Guide](https://owasp.org/www-project-web-security-testing-guide/)
- [WebRTC Security Architecture (RFC 7201)](https://datatracker.ietf.org/doc/html/rfc7201)
- [NIST Password Guidelines (SP 800-63B)](https://pages.nist.gov/800-63-3/sp800-63b.html)
- [Rust Security Guidelines](https://doc.rust-lang.org/nomicon/security.html)
- [Go Security Best Practices](https://go.dev/doc/security)
