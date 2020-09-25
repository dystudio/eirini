package cmd_test

import (
	"os"

	"code.cloudfoundry.org/eirini"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
)

var _ = Describe("MetricsCollector", func() {
	var (
		config         *eirini.MetricsCollectorConfig
		configFilePath string
		session        *gexec.Session
	)
	BeforeEach(func() {
		config = &eirini.MetricsCollectorConfig{
			KubeConfig: eirini.KubeConfig{
				ConfigPath: pathToTestFixture("kube.conf"),
			},
			LoggregatorCAPath:   pathToTestFixture("cert"),
			LoggregatorCertPath: pathToTestFixture("cert"),
			LoggregatorKeyPath:  pathToTestFixture("key"),
		}
	})

	JustBeforeEach(func() {
		session, configFilePath = eiriniBins.MetricsCollector.Run(config)
	})

	AfterEach(func() {
		if configFilePath != "" {
			Expect(os.Remove(configFilePath)).To(Succeed())
		}
		if session != nil {
			Eventually(session.Kill()).Should(gexec.Exit())
		}
	})

	It("should be able to start properly", func() {
		Consistently(session, "5s").ShouldNot(gexec.Exit())
	})

	When("the config file doesn't exist", func() {
		It("exits reporting missing config file", func() {
			session = eiriniBins.MetricsCollector.Restart("/does/not/exist", session)
			Eventually(session).Should(gexec.Exit())
			Expect(session.ExitCode).ToNot(BeZero())
			Expect(session.Err).To(gbytes.Say("failed to read file"))
		})
	})

	When("the config file is not valid yaml", func() {
		It("exits reporting missing config file", func() {
			session = eiriniBins.MetricsCollector.Restart(pathToTestFixture("invalid.yml"), session)
			Eventually(session).Should(gexec.Exit())
			Expect(session.ExitCode).ToNot(BeZero())
			Expect(session.Err).To(gbytes.Say("failed to unmarshal yaml"))
		})
	})

	When("no TLS certs are provided", func() {
		BeforeEach(func() {
			config.LoggregatorCAPath = ""
			config.LoggregatorCertPath = ""
			config.LoggregatorKeyPath = ""
		})
		It("exits reporting missing config file", func() {
			Eventually(session).Should(gexec.Exit())
			Expect(session.ExitCode).ToNot(BeZero())
			Expect(session.Err).To(gbytes.Say("open : no such file or directory"))
		})
	})

	Context("When the loggregator CA file is missing", func() {
		BeforeEach(func() {
			config = metricsCollectorConfig()
			config.LoggregatorCAPath = "/somewhere/over/the/rainbow"
		})

		It("should exit with a useful error message", func() {
			Eventually(session.Exited).Should(BeClosed())
			Expect(session.ExitCode()).To(Equal(2))
			Expect(session.Err).Should(gbytes.Say(`"Loggregator CA" file does not exist`))
		})
	})

	Context("When the loggregator cert file is missing", func() {
		BeforeEach(func() {
			config = metricsCollectorConfig()
			config.LoggregatorCertPath = "/somewhere/over/the/rainbow"
		})

		It("should exit with a useful error message", func() {
			Eventually(session.Exited).Should(BeClosed())
			Expect(session.ExitCode()).To(Equal(2))
			Expect(session.Err).Should(gbytes.Say(`"Loggregator Cert" file does not exist`))
		})
	})

	Context("When the loggregator key file is missing", func() {
		BeforeEach(func() {
			config = metricsCollectorConfig()
			config.LoggregatorKeyPath = "/somewhere/over/the/rainbow"
		})

		It("should exit with a useful error message", func() {
			Eventually(session.Exited).Should(BeClosed())
			Expect(session.ExitCode()).To(Equal(2))
			Expect(session.Err).Should(gbytes.Say(`"Loggregator Key" file does not exist`))
		})
	})

	Context("When the loggregator CA file is invalid", func() {
		BeforeEach(func() {
			config = metricsCollectorConfig()
			config.LoggregatorCAPath = pathToTestFixture("kube.conf")
		})

		It("should exit with a useful error message", func() {
			Eventually(session.Exited).Should(BeClosed())
			Expect(session.ExitCode()).To(Equal(2))
			Expect(session.Err).Should(gbytes.Say(`Failed to create loggregator tls config: cannot parse ca cert`))
		})
	})
})
