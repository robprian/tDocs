# 13. Built-in ACME HTTPS for Custom Domains

We decided to terminate HTTPS inside tDocs with `golang.org/x/crypto/acme/autocert`
(a subpackage of the existing `golang.org/x/crypto` dependency) instead of
bundling an external reverse-proxy binary.

The operator enters one domain in Settings (DB keys `public_domain`,
`acme_email`); tDocs answers the ACME HTTP-01 challenge on port 80 and serves
HTTPS on 443. Only DNS has to point at the server. Certificates cache in the
data dir (`<DataDir>/acme`) so restarts reuse them within Let's Encrypt rate
limits.

Plain-HTTP dashboard behavior is unchanged when no domain is set, so a failed
ACME setup never makes the appliance unreachable. The systemd unit gains only
`CAP_NET_BIND_SERVICE` for ports 80/443; the existing manual-cert
`TDOCS_TLS_CERT_FILE` path is untouched.
