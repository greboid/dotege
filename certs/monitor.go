package certs

import (
	"github.com/csmith/dotege/model"
	"go.uber.org/zap"
	"io/ioutil"
	"path"
	"strings"
)

// CertificateMonitor handles scanning for new/updated certificates
type CertificateMonitor struct {
	logger      *zap.SugaredLogger
	channel     chan<- model.FoundCertificate
	directories []string
	certs       map[string]*model.FoundCertificate
}

// NewCertificateManager creates a new CertificateMonitor.
func NewCertificateManager(logger *zap.SugaredLogger, channel chan<- model.FoundCertificate) *CertificateMonitor {
	return &CertificateMonitor{
		logger:  logger,
		channel: channel,
		certs:   make(map[string]*model.FoundCertificate),
	}
}

// AddDirectory adds a new directory to monitor
func (c *CertificateMonitor) AddDirectory(directory string) {
	c.logger.Infof("Adding certificate directory %s", directory)
	c.directories = append(c.directories, directory)
	go c.scanForFolders(directory)
}

func (c *CertificateMonitor) scanForFolders(dir string) {
	c.logger.Debugf("Scanning folder %s for certificates", dir)
	dirs, err := ioutil.ReadDir(dir)
	if err != nil {
		c.logger.Errorf("Unable to read directory %s - %s", dir, err.Error())
		return
	}

	for _, d := range dirs {
		if d.IsDir() {
			c.scanForCerts(d.Name(), path.Join(dir, d.Name()))
		}
	}
}

func (c *CertificateMonitor) scanForCerts(vhost string, dir string) {
	c.logger.Debugf("Scanning folder %s for certificates for %s", dir, vhost)
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		c.logger.Errorf("Unable to read directory %s - %s", dir, err.Error())
		return
	}

	cert := model.FoundCertificate{}
	for _, f := range files {
		ext := path.Ext(f.Name())
		base := path.Base(f.Name())
		base = base[:len(base)-len(ext)]
		if ext == ".pem" {
			prefix := strings.Split(base, "-")[0]
			added := maybeAddPart(&cert, prefix, path.Join(dir, f.Name()))
			if added && f.ModTime().After(cert.ModTime) {
				cert.ModTime = f.ModTime()
			}
		}
	}

	c.maybeAddCert(vhost, cert)
}

func maybeAddPart(cert *model.FoundCertificate, part string, path string) bool {
	switch part {
	case "cert":
		cert.Cert = path
	case "chain":
		cert.Chain = path
	case "fullchain":
		cert.FullChain = path
	case "privkey":
		cert.PrivateKey = path
	default:
		return false
	}
	return true
}

func (c *CertificateMonitor) maybeAddCert(vhost string, cert model.FoundCertificate) {
	if len(cert.Cert) > 0 && len(cert.Chain) > 0 && len(cert.FullChain) > 0 && len(cert.PrivateKey) > 0 {
		if existing, ok := c.certs[vhost]; ok {
			if cert.ModTime.After(existing.ModTime) {
				c.logger.Debugf("Found newer certificate files for %s in %s", vhost, path.Dir(cert.Cert))
				c.certs[vhost] = &cert
				c.channel <- cert
			}
		} else {
			c.logger.Debugf("Found new certificate files for %s in %s", vhost, path.Dir(cert.Cert))
			c.certs[vhost] = &cert
			c.channel <- cert
		}
	}
}
