package dotege

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
	"time"
)

// SavedCertificate is a certificate we've previously obtained and saved for future use.
type SavedCertificate struct {
	Issuer      []byte `json:"issuer"`
	PrivateKey  []byte `json:"privateKey"`
	Certificate []byte `json:"certificate"`

	Subject  string    `json:"subject"`
	AltNames []string  `json:"altNames"`
	NotAfter time.Time `json:"notAfter"`

	OcspResponse   []byte    `json:"ocspResponse"`
	NextOcspUpdate time.Time `json:"nextOcspUpdate"`
}

// ValidFor indicates whether the certificate will be valid for the entirety of the given period.
func (s *SavedCertificate) ValidFor(period time.Duration) bool {
	return s.NotAfter.After(time.Now().Add(period))
}

// HasStapleFor indicates whether the OCSP staple covers the entirety of the given period.
func (s *SavedCertificate) HasStapleFor(period time.Duration) bool {
	return s.NextOcspUpdate.After(time.Now().Add(period))
}

// IsFor determines whether this certificate covers the given subject and altNames (and no more).
func (s *SavedCertificate) IsFor(subject string, altNames []string) bool {
	if s.Subject != subject || len(s.AltNames) != len(altNames) {
		return false
	}

	// Create copies of the names, so we can in-place sort them without mutating random caller data.
	altNames1 := append([]string(nil), s.AltNames...)
	altNames2 := append([]string(nil), altNames...)
	sort.Strings(altNames1)
	sort.Strings(altNames2)

	for i := range altNames1 {
		if altNames1[i] != altNames2[i] {
			return false
		}
	}

	return true
}

// CertificateStore is responsible for storing and managing certificates. It can save and load data to/from a file.
type CertificateStore struct {
	path string

	certificates []*SavedCertificate
}

// NewCertificateStore creates a new certificate store, using the specified path for storage.
func NewCertificateStore(path string) *CertificateStore {
	return &CertificateStore{
		path: path,
	}
}

// Load attempts to load the current store from disk. If the file is not found, no error is returned.
func (s *CertificateStore) Load() error {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}

	return json.Unmarshal(b, &s.certificates)
}

// Save serialises the current store to disk.
func (s *CertificateStore) Save() error {
	s.pruneCertificates()

	b, err := json.Marshal(s.certificates)
	if err != nil {
		return err
	}

	return os.WriteFile(s.path, b, 0600)
}

// GetCertificate returns a previously stored certificate with the given subject and alt names, or `nil` if none exists.
//
// Returned certificates are not guaranteed to be valid.
func (s *CertificateStore) GetCertificate(subjectName string, altNames []string) *SavedCertificate {
	for i := range s.certificates {
		if s.certificates[i].IsFor(subjectName, altNames) {
			return s.certificates[i]
		}
	}

	return nil
}

// removeCertificate removes any previously stored certificate with the given subject and alt names.
func (s *CertificateStore) removeCertificate(subjectName string, altNames []string) {
	for i := range s.certificates {
		if s.certificates[i].IsFor(subjectName, altNames) {
			s.certificates = append(s.certificates[:i], s.certificates[i+1:]...)
			return
		}
	}
}

// pruneCertificates removes any certificates that are no longer valid.
func (s *CertificateStore) pruneCertificates() {
	savedCerts := s.certificates[:0]
	for i := range s.certificates {
		if s.certificates[i].ValidFor(0) {
			savedCerts = append(savedCerts, s.certificates[i])
		}
	}
	s.certificates = savedCerts
}

// SaveCertificate adds the given certificate to the store. Any previously saved certificates for the same subject
// and alt names will be removed. The store will be saved to disk after the certificate is added.
func (s *CertificateStore) SaveCertificate(certificate *SavedCertificate) error {
	s.removeCertificate(certificate.Subject, certificate.AltNames)
	s.certificates = append(s.certificates, certificate)
	return s.Save()
}
