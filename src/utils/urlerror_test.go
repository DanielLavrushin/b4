package utils

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"syscall"
	"testing"
)

const askedURL = "https://canaryuser:canarypass@files.example/canarypath?token=canaryquery"

func TestAFailureWithoutServerTextStaysWhatItIs(t *testing.T) {
	if err := URLErrorCause(askedURL, nil); err != nil {
		t.Errorf("no failure became %v", err)
	}
	refused := fmt.Errorf("dial tcp 192.0.2.1:443: connect: %w", syscall.ECONNREFUSED)
	for _, err := range []error{refused, &url.Error{Op: "Get", URL: askedURL, Err: refused}} {
		if cause := URLErrorCause(askedURL, err); cause != refused {
			t.Errorf("%v became %v", err, cause)
		}
	}
}

func TestACertificateFailureKeepsItsReason(t *testing.T) {
	unknown := x509.UnknownAuthorityError{}
	failure := &tls.CertificateVerificationError{Err: fmt.Errorf("%w (possibly because of %q)", unknown, "the CA")}
	cause := URLErrorCause(askedURL, &url.Error{Op: "Get", URL: askedURL, Err: failure})
	if cause != error(failure) || !strings.Contains(cause.Error(), `"the CA"`) {
		t.Errorf("the reason of a certificate failure is not the server's reply, got %v", cause)
	}
}

func TestTextTheServerSentIsCut(t *testing.T) {
	for text, want := range map[string]string{
		`malformed HTTP response "canaryquery"`: `malformed HTTP response "..."`,
		`net/http: HTTP/1.x transport connection broken: malformed HTTP status code "/canarypath?token=canaryquery"`: `net/http: HTTP/1.x transport connection broken: malformed HTTP status code "..."`,
		`malformed MIME header: missing colon: "Basic Y2FuYXJ5"`:                                                     `malformed MIME header: missing colon: "..."`,
		`unsupported protocol scheme "canary"`:                                                                       `unsupported protocol scheme "..."`,
		`http2: server sent GOAWAY and closed the connection; debug="canary"`:                                        `http2: server sent GOAWAY and closed the connection; debug="..."`,
		`too many transfer encodings: ["canary" "canarypath"]`:                                                       `too many transfer encodings: ["..."`,
		`failed to parse Location header "http://[canaryhost/": missing ']'`:                                         ErrURLRedirect.Error(),
	} {
		for _, err := range []error{errors.New(text), &url.Error{Op: "Get", URL: askedURL, Err: errors.New(text)}} {
			if cause := URLErrorCause(askedURL, err); cause.Error() != want {
				t.Errorf("%v became %q, want %q", err, cause, want)
			}
		}
	}
}

func TestAnAddressThatDoesNotParseIsNotRepeated(t *testing.T) {
	for _, asked := range []string{"https://canaryuser:canary pass@files.example/", "https://canaryuser:123456789/canarypath"} {
		err := &url.Error{Op: "Get", URL: asked, Err: errors.New("invalid port :123456789")}
		if cause := URLErrorCause(asked, err); cause != ErrURLSyntax {
			t.Errorf("%q: %v", asked, cause)
		}
	}
}
