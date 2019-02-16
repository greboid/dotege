package certs

import (
	"github.com/csmith/dotege/model"
	"go.uber.org/zap"
	"io/ioutil"
	"os"
	"path"
	"time"
)

// CertificateDeployer deploys certificates according to their configuration.
type CertificateDeployer struct {
	logger        *zap.SugaredLogger
	certChannel   <-chan model.FoundCertificate
	deployChannel chan bool
	certs         map[string]model.FoundCertificate
	hostnames     map[string]*model.Hostname
}

// NewCertificateDeployer creates a new CertificateDeployer.
func NewCertificateDeployer(logger *zap.SugaredLogger, channel <-chan model.FoundCertificate) *CertificateDeployer {
	deployer := &CertificateDeployer{
		logger:        logger,
		certChannel:   channel,
		deployChannel: make(chan bool, 1),
		certs:         make(map[string]model.FoundCertificate),
	}

	go deployer.monitor()
	go deployer.deployAll()

	return deployer
}

func (c *CertificateDeployer) monitor() {
	for {
		select {
		case cert := <-c.certChannel:
			c.certs[cert.Hostname] = cert
			c.deployChannel <- true
		}
	}
}

func (c *CertificateDeployer) deployAll() {
	for {
		select {
		case <-c.deployChannel:
			c.logger.Debug("Checking for certificates requiring deployment")
			for _, hostname := range c.hostnames {
				if cert, ok := c.certs[hostname.Name]; ok {
					c.deploySingle(cert, hostname)
				} else {
					c.logger.Warnf("No certificate found for %s", hostname.Name)
				}
			}
		}
	}
}

func (c *CertificateDeployer) deploySingle(cert model.FoundCertificate, hostname *model.Hostname) {
	if (hostname.CertActions & model.COMBINE) == model.COMBINE {
		chain := c.readFile(cert.FullChain)
		pkey := c.readFile(cert.PrivateKey)
		c.deployFile("combined.pem", append(chain, pkey...), cert.ModTime, hostname)
	} else {
		c.deployFile("cert.pem", c.readFile(cert.Cert), cert.ModTime, hostname)
		c.deployFile("chain.pem", c.readFile(cert.Chain), cert.ModTime, hostname)
		c.deployFile("fullchain.pem", c.readFile(cert.FullChain), cert.ModTime, hostname)
		c.deployFile("privkey.pem", c.readFile(cert.PrivateKey), cert.ModTime, hostname)
	}
}

func (c *CertificateDeployer) deployFile(name string, content []byte, modTime time.Time, hostname *model.Hostname) {
	var target string
	if (hostname.CertActions & model.FLATTEN) == model.FLATTEN {
		target = path.Join(hostname.CertDestination, hostname.Name+".pem")
	} else {
		target = path.Join(hostname.CertDestination, hostname.Name, name)
	}

	info, err := os.Stat(target)
	if err == nil && info.ModTime().After(modTime) {
		c.logger.Debugf("Not writing %s as it was modified more recently than our cert", target)
		return
	}

	err = ioutil.WriteFile(target, content, 0700)
	if err != nil {
		c.logger.Warnf("Unable to write certificate %s - %s", target, err.Error())
	} else {
		c.logger.Infof("Updated certificate file %s", target)
	}
}

func (c *CertificateDeployer) readFile(name string) []byte {
	data, err := ioutil.ReadFile(name)
	if err != nil {
		c.logger.Errorf("Unable to read certificate file %s - %s", name, err.Error())
	}
	return data
}

func (c *CertificateDeployer) UpdateHostnames(hostnames map[string]*model.Hostname) {
	c.hostnames = hostnames
	c.deployChannel <- true
}
