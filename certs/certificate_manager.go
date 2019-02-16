package certs

import (
	"go.uber.org/zap"
	"io/ioutil"
	"path"
	"strings"
	"time"
)

// CertificateManager handles scanning for new/updated certificates and deploying them to a destination.
type CertificateManager struct {
	logger      *zap.SugaredLogger
	directories []string
	certs       map[string]*foundCertificate
}

type foundCertificate struct {
	cert       string
	chain      string
	fullChain  string
	privateKey string
	modTime    time.Time
}

// NewCertificateManager creates a new CertificateManager.
func NewCertificateManager(logger *zap.SugaredLogger) *CertificateManager {
	return &CertificateManager{
		logger: logger,
	}
}

// AddDirectory adds a new directory to monitor
func (c *CertificateManager) AddDirectory(directory string) {
	c.directories = append(c.directories, directory)
	go c.scanForFolders(directory)
}

func (c *CertificateManager) scanForFolders(dir string) {
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

func (c *CertificateManager) scanForCerts(vhost string, dir string) {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		c.logger.Errorf("Unable to read directory %s - %s", dir, err.Error())
		return
	}

	cert := foundCertificate{}
	for _, f := range files {
		ext := path.Ext(f.Name())
		base := path.Base(f.Name())
		if ext == "" && strings.Contains(base, "-") {
			switch parts := strings.Split(base, "-"); parts[0] {
			case "cert":
				cert.cert = path.Join(dir, f.Name())
			case "chain":
				cert.chain = path.Join(dir, f.Name())
			case "fullchain":
				cert.fullChain = path.Join(dir, f.Name())
			case "privkey":
				cert.privateKey = path.Join(dir, f.Name())
			default:
				continue
			}

			if f.ModTime().After(cert.modTime) {
				cert.modTime = f.ModTime()
			}
		}
	}

	c.maybeAddCert(vhost, cert)
}

func (c *CertificateManager) maybeAddCert(vhost string, cert foundCertificate) {
	if len(cert.cert) > 0 && len(cert.chain) > 0 && len(cert.fullChain) > 0 && len(cert.privateKey) > 0 {
		if existing, ok := c.certs[vhost]; ok {
			if cert.modTime.After(existing.modTime) {
				c.logger.Debugf("Found newer certificate files for %s in %s", vhost, path.Dir(cert.cert))
				c.certs[vhost] = &cert
			}
		} else {
			c.logger.Debugf("Found new certificate files for %s in %s", vhost, path.Dir(cert.cert))
			c.certs[vhost] = &cert
		}
	}
}
