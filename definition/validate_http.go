package definition

import (
	"encoding/pem"
	"strings"
)

// Values the engines accept for http.version and tls.min_version. Both
// keys are optional: an empty value is absent and is not checked.
var (
	httpVersions = []string{"1.1"}
	tlsVersions  = []string{"1.0", "1.1", "1.2", "1.3"}
)

// pemBegin starts every PEM block.
const pemBegin = "-----BEGIN"

// pemKeySuffix ends the PEM block type of every private key ("PRIVATE KEY",
// "RSA PRIVATE KEY", "EC PRIVATE KEY", ...).
const pemKeySuffix = "PRIVATE KEY"

// IsInlinePEM reports whether a client_cert, client_key or ca_cert value is
// inline PEM (it contains "-----BEGIN") rather than a file path.
func IsInlinePEM(s string) bool {
	return strings.Contains(s, pemBegin)
}

// httpFollows reports whether redirects are followed for steps that do not
// set follow_redirects.
func httpFollows(d *Definition) bool {
	if d.HTTP != nil && d.HTTP.FollowRedirects != nil {
		return *d.HTTP.FollowRedirects
	}
	return true
}

// validateHTTPTLS checks the test-level http and tls objects.
func validateHTTPTLS(ps *problems, d *Definition) {
	if h := d.HTTP; h != nil {
		ps.maxRedirects("http.max_redirects", h.MaxRedirects)
		if h.Version != "" {
			ps.oneOf("http.version", h.Version, httpVersions)
		}
	}
	t := d.TLS
	if t == nil {
		return
	}
	if t.MinVersion != "" {
		ps.oneOf("tls.min_version", t.MinVersion, tlsVersions)
	}
	// Messages for the three PEM fields never repeat the value: it can be
	// a private key.
	ps.pemField("tls.client_cert", t.ClientCert, pemClientCert)
	if t.ClientCert == "" && t.ClientKey != "" {
		ps.add("tls.client_cert", "is required with client_key")
	}
	ps.pemField("tls.client_key", t.ClientKey, pemPrivateKey)
	if t.ClientKey == "" && t.ClientCert != "" {
		ps.add("tls.client_key", "is required with client_cert")
	}
	ps.pemField("tls.ca_cert", t.CACert, pemCACert)
}

// validateStepRedirects checks a step's redirect overrides at path sp.
// follows is the test-level follow_redirects, after defaults.
func validateStepRedirects(ps *problems, sp string, st Step, follows bool) {
	if st.FollowRedirects != nil {
		follows = *st.FollowRedirects
	}
	switch {
	case st.MaxRedirects < 0:
		ps.maxRedirects(sp+".max_redirects", st.MaxRedirects)
	case st.MaxRedirects > 0 && !follows:
		ps.add(sp+".max_redirects", "has no effect: redirects are not followed for this step; set follow_redirects: true on the step")
	}
}

func (ps *problems) maxRedirects(path string, n int) {
	if n < 0 {
		ps.add(path, "must not be negative; to stop following redirects, set follow_redirects: false")
	}
}

// pemKind is what an inline PEM field must hold.
type pemKind int

const (
	pemClientCert pemKind = iota // one or more CERTIFICATE blocks, no private key
	pemCACert                    // the same, for ca_cert
	pemPrivateKey                // an unencrypted private key block
)

// pemField checks a value that is a file path or inline PEM. Its messages
// never include the value or any part of it, block types included.
func (ps *problems) pemField(path, v string, kind pemKind) {
	if v == "" {
		return
	}
	if strings.TrimSpace(v) == "" {
		ps.add(path, "must not be blank")
		return
	}
	if !IsInlinePEM(v) {
		if strings.ContainsAny(v, "\n\r") {
			ps.add(path, "must be a file path or inline PEM with a -----BEGIN line")
		}
		return
	}
	var blocks, certs, keys int
	var key *pem.Block
	rest := []byte(v)
	for {
		var b *pem.Block
		b, rest = pem.Decode(rest)
		if b == nil {
			break
		}
		blocks++
		switch {
		case b.Type == "CERTIFICATE":
			certs++
		case isPEMKeyType(b.Type):
			if keys == 0 {
				key = b
			}
			keys++
		}
	}
	if blocks == 0 {
		ps.add(path, "must be valid PEM: no complete -----BEGIN/-----END block found")
		return
	}
	switch kind {
	case pemClientCert, pemCACert:
		switch {
		case keys > 0 && kind == pemClientCert:
			ps.add(path, "must not contain a private key; put the key in client_key")
		case keys > 0:
			ps.add(path, "must not contain a private key")
		case certs == 0:
			ps.add(path, "must contain a PEM CERTIFICATE block")
		}
	case pemPrivateKey:
		switch {
		case key == nil:
			ps.add(path, "must contain a PEM private key block")
		case key.Type == "ENCRYPTED "+pemKeySuffix,
			strings.Contains(key.Headers["Proc-Type"], "ENCRYPTED"):
			ps.add(path, "encrypted keys are not supported")
		}
	}
}

// isPEMKeyType reports whether a PEM block type is a private key, by the
// same rule the Go standard library uses when it loads a key pair.
func isPEMKeyType(t string) bool {
	return t == pemKeySuffix || strings.HasSuffix(t, " "+pemKeySuffix)
}
