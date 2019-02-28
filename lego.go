package main

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"github.com/xenolf/lego/certcrypto"
	"github.com/xenolf/lego/certificate"
	"github.com/xenolf/lego/lego"
	"github.com/xenolf/lego/log"
	"github.com/xenolf/lego/providers/dns"
	"github.com/xenolf/lego/registration"
	"go.uber.org/zap"
	"io/ioutil"
	"sort"
	"time"
)

type AcmeUser struct {
	Email        string                 `json:"email"`
	Registration *registration.Resource `json:"registration,omitempty"`
	LiveKey      *ecdsa.PrivateKey      `json:"-"`
	Key          []byte                 `json:"key"`
}

func (u *AcmeUser) GetEmail() string {
	return u.Email
}
func (u AcmeUser) GetRegistration() *registration.Resource {
	return u.Registration
}
func (u *AcmeUser) GetPrivateKey() crypto.PrivateKey {
	return u.LiveKey
}

type SavedCertificate struct {
	Domains           []string  `json:"domains"`
	CertURL           string    `json:"certUrl"`
	CertStableURL     string    `json:"certStableUrl"`
	NotAfter          time.Time `json:"notAfter"`
	PrivateKey        []byte    `json:"privateKey"`
	Certificate       []byte    `json:"certificate"`
	IssuerCertificate []byte    `json:"issuer"`
	CSR               []byte    `json:"csr"`
}

type CertificateManagerData struct {
	User  *AcmeUser           `json:"user"`
	Certs []*SavedCertificate `json:"certs"`
}

type CertificateManager struct {
	logger       *zap.SugaredLogger
	acmeProvider string
	keyType      certcrypto.KeyType
	path         string
	dnsProvider  string
	data         *CertificateManagerData
	client       *lego.Client
}

func NewCertificateManager(logger *zap.SugaredLogger, acmeProvider string, keyType certcrypto.KeyType, dnsProvider string, path string) *CertificateManager {
	return &CertificateManager{
		logger:       logger,
		acmeProvider: acmeProvider,
		keyType:      keyType,
		dnsProvider:  dnsProvider,
		path:         path,
	}
}

func (c *CertificateManager) Init(email string) error {
	legoLogger, err := zap.NewStdLogAt(c.logger.Desugar(), zap.DebugLevel)
	if err == nil {
		log.Logger = legoLogger
		err = c.load()
	}
	if err == nil {
		err = c.createUser(email)
	}
	if err == nil {
		err = c.createClient()
	}
	if err == nil {
		err = c.register()
	}
	return err
}

func (c *CertificateManager) load() error {
	data := &CertificateManagerData{}
	buf, _ := ioutil.ReadFile(c.path)
	if buf != nil {
		err := json.Unmarshal(buf, data)
		if err != nil {
			return err
		}

		if data.User != nil {
			liveKey, err := x509.ParseECPrivateKey(data.User.Key)
			if err != nil {
				return err
			}
			data.User.LiveKey = liveKey
		}
	}
	c.data = data
	return nil
}

func (c *CertificateManager) save() error {
	c.logger.Info("Saving certificate config to ", c.path)
	data, err := json.Marshal(c.data)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(c.path, data, 0600)
}

func (c *CertificateManager) createUser(email string) error {
	if c.data.User == nil {
		c.logger.Infof("Creating a new private key for ACME use")
		privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return err
		}

		marshaled, err := x509.MarshalECPrivateKey(privateKey)
		if err != nil {
			return err
		}

		c.data.User = &AcmeUser{
			LiveKey: privateKey,
			Key:     marshaled,
			Email:   email,
		}
		return c.save()
	}
	return nil
}

func (c *CertificateManager) createClient() error {
	config := lego.NewConfig(c.data.User)

	config.CADirURL = c.acmeProvider
	config.Certificate.KeyType = c.keyType

	client, err := lego.NewClient(config)
	if err != nil {
		return err
	}

	provider, err := dns.NewDNSChallengeProviderByName(c.dnsProvider)
	if err != nil {
		return err
	}

	err = client.Challenge.SetDNS01Provider(provider)
	if err != nil {
		return err
	}

	c.client = client
	return nil
}

func (c *CertificateManager) register() error {
	if c.data.User.Registration == nil {
		c.logger.Infof("Registering new user with ACME provider")
		reg, err := c.client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
		if err != nil {
			return err
		}
		c.data.User.Registration = reg
		return c.save()
	}
	return nil
}

func (c *CertificateManager) GetCertificate(domains []string) (error, *SavedCertificate) {
	existing := c.loadCert(domains)
	if existing != nil {
		c.logger.Debugf("Returning existing certificate for request %s", domains)
		return nil, existing
	}

	request := certificate.ObtainRequest{
		Domains: domains,
		Bundle:  true,
	}
	cert, err := c.client.Certificate.Obtain(request)
	if err != nil {
		return err, nil
	}
	return c.saveCert(domains, cert)
}

func (c *CertificateManager) loadCert(domains []string) *SavedCertificate {
	for _, cert := range c.data.Certs {
		if domainsMatch(cert.Domains, domains) {
			return cert
		}
	}
	return nil
}

func domainsMatch(domains1, domains2 []string) bool {
	if len(domains1) != len(domains2) {
		return false
	}
	if domains1[0] != domains2[0] {
		return false
	}
	sort.Strings(domains1)
	sort.Strings(domains2)
	for i := range domains1 {
		if domains1[i] != domains2[i] {
			return false
		}
	}

	return true
}

func (c *CertificateManager) saveCert(domains []string, cert *certificate.Resource) (error, *SavedCertificate) {
	savedCert := &SavedCertificate{
		Domains:           domains,
		Certificate:       cert.Certificate,
		NotAfter:          c.getExpiry(cert),
		PrivateKey:        cert.PrivateKey,
		CertStableURL:     cert.CertStableURL,
		CertURL:           cert.CertURL,
		CSR:               cert.CSR,
		IssuerCertificate: cert.IssuerCertificate,
	}
	c.data.Certs = append(c.data.Certs, savedCert)
	return c.save(), savedCert
}

func (c *CertificateManager) getExpiry(cert *certificate.Resource) time.Time {
	pem, err := certcrypto.ParsePEMCertificate(cert.Certificate)
	if err != nil {
		c.logger.Fatal(err)
	}

	return pem.NotAfter
}
