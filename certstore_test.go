package dotege

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSavedCertificate_ValidFor(t *testing.T) {
	tests := []struct {
		name     string
		notAfter time.Time
		period   time.Duration
		want     bool
	}{
		{"Valid for long period", time.Now().Add(time.Hour * 24 * 365 * 10), time.Hour, true},
		{"Valid for short period", time.Now().Add(time.Hour + time.Minute), time.Hour, true},
		{"Expired in the past", time.Now().Add(-time.Hour), time.Hour, false},
		{"Expires in the period", time.Now().Add(time.Minute * 30), time.Hour, false},
		{"Zero value time", time.Time{}, time.Hour, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &SavedCertificate{
				NotAfter: tt.notAfter,
			}
			if got := s.ValidFor(tt.period); got != tt.want {
				t.Errorf("ValidFor() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSavedCertificate_HasStapleFor(t *testing.T) {
	tests := []struct {
		name       string
		nextUpdate time.Time
		period     time.Duration
		want       bool
	}{
		{"Valid for long period", time.Now().Add(time.Hour * 24 * 365 * 10), time.Hour, true},
		{"Valid for short period", time.Now().Add(time.Hour + time.Minute), time.Hour, true},
		{"Expired in the past", time.Now().Add(-time.Hour), time.Hour, false},
		{"Expires in the period", time.Now().Add(time.Minute * 30), time.Hour, false},
		{"Zero value time", time.Time{}, time.Hour, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &SavedCertificate{
				NextOcspUpdate: tt.nextUpdate,
			}
			if got := s.HasStapleFor(tt.period); got != tt.want {
				t.Errorf("HasStapleFor() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSavedCertificate_IsFor(t *testing.T) {
	type args struct {
		subject  string
		altNames []string
	}
	tests := []struct {
		name       string
		certNames  args
		queryNames args
		want       bool
	}{
		{"Subject only, matches", args{subject: "example.com"}, args{subject: "example.com"}, true},
		{"Subject only, doesn't match", args{subject: "example.com"}, args{subject: "example.org"}, false},
		{"Subject and alt, matching", args{"example.com", []string{"example.org"}}, args{"example.com", []string{"example.org"}}, true},
		{"Subject and alt, swapped", args{"example.com", []string{"example.org"}}, args{"example.org", []string{"example.com"}}, false},
		{"Subject and alt, alt doesn't match", args{"example.com", []string{"example.org"}}, args{"example.com", []string{"example.net"}}, false},
		{"Subject and alt, checked against only subject", args{"example.com", []string{"example.org"}}, args{subject: "example.com"}, false},
		{"Subject only, checked against subject and alt", args{subject: "example.com"}, args{"example.com", []string{"example.org"}}, false},
		{"Extra alt in query", args{"example.com", []string{"example.org"}}, args{"example.com", []string{"example.org", "example.net"}}, false},
		{"Extra alt in cert", args{"example.com", []string{"example.org", "example.net"}}, args{"example.com", []string{"example.org"}}, false},
		{"Multiple alts, matching", args{"example.com", []string{"example.org", "example.net"}}, args{"example.com", []string{"example.org", "example.net"}}, true},
		{"Multiple alts, different order", args{"example.com", []string{"example.org", "example.net"}}, args{"example.com", []string{"example.net", "example.org"}}, true},
		{"Multiple alts, not matching", args{"example.com", []string{"example.org", "example.net"}}, args{"example.com", []string{"example.org", "example.xyz"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &SavedCertificate{
				Subject:  tt.certNames.subject,
				AltNames: tt.certNames.altNames,
			}
			if got := s.IsFor(tt.queryNames.subject, tt.queryNames.altNames); got != tt.want {
				t.Errorf("IsFor() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCertificateStore_LoadSaveGet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "certs.json")
	store := NewCertificateStore(path)
	if err := store.Load(); err != nil {
		t.Errorf("Load() = %v, want nil", err)
	}

	timestamp := time.Now().Add(time.Hour).UTC()

	cert := &SavedCertificate{
		Issuer:         []byte("this is the issuer"),
		PrivateKey:     []byte("this is the private key"),
		Certificate:    []byte("this is the cert"),
		Subject:        "subject.example.com",
		AltNames:       []string{"alt1.example.com", "alt2.example.com"},
		NotAfter:       timestamp,
		OcspResponse:   []byte("this is the ocsp response"),
		NextOcspUpdate: timestamp.Add(time.Minute),
	}

	if err := store.SaveCertificate(cert); err != nil {
		t.Errorf("SaveCertificate() = %v, want nil", err)
	}

	newStore := NewCertificateStore(path)
	if err := newStore.Load(); err != nil {
		t.Errorf("newStore.Load() = %v, want nil", err)
	}

	newCert := newStore.GetCertificate(cert.Subject, cert.AltNames)
	if !reflect.DeepEqual(newCert, cert) {
		t.Errorf("newStore.GetCertificate() = %#v, want %#v", newCert, cert)
	}
}

func TestCertificateStore_PrunesExpiredCertsOnSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "certs.json")
	store := NewCertificateStore(path)

	certs := []*SavedCertificate{
		{
			Subject:  "just-expired.example.com",
			NotAfter: time.Now().Add(-time.Hour),
		},
		{
			Subject:  "long-expired.example.com",
			NotAfter: time.Now().Add(-time.Hour * 24 * 365),
		},
		{
			Subject:  "zero-time.example.com",
			NotAfter: time.Time{},
		},
		{
			Subject:  "just-valid.example.com",
			NotAfter: time.Now().Add(time.Hour),
		},
		{
			Subject:  "long-valid.example.com",
			NotAfter: time.Now().Add(time.Hour * 24 * 365),
		},
	}

	for i := range certs {
		if err := store.SaveCertificate(certs[i]); err != nil {
			t.Errorf("SaveCertificate() = %v, want nil", err)
		}
	}

	for i := range certs {
		t.Run(certs[i].Subject, func(t *testing.T) {
			hasCert := store.GetCertificate(certs[i].Subject, certs[i].AltNames) != nil
			expectedCert := strings.Contains(certs[i].Subject, "-valid")
			if hasCert != expectedCert {
				t.Errorf("GetCertificate() = %v, want %v", hasCert, expectedCert)
			}
		})
	}
}
