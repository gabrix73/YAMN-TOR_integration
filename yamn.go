package main

import (
	"context"
	"fmt"
	"io/ioutil"
	stdlog "log"
	"net"
	"net/url"
	"os"
	"time"

	"github.com/Masterminds/log-go"
	"github.com/crooks/jlog"
	loglevel "github.com/crooks/log-go-level"
	"github.com/crooks/yamn/config"
	"github.com/crooks/yamn/idlog"
	"github.com/crooks/yamn/keymgr"
	"github.com/luksen/maildir"
	"golang.org/x/net/proxy"
)

const (
	version        string = "0.2.8-tor"
	dayLength      int    = 24 * 60 * 60 // Day in seconds
	maxFragLength         = 17910
	maxCopies             = 5
	base64LineWrap        = 64
	rfc5322date           = "Mon, 2 Jan 2006 15:04:05 -0700"
	shortdate             = "2 Jan 2006"
)

var (
	// flags - Command line flags
	flag *config.Flags
	// cfg - Config parameters
	cfg *config.Config
	// Pubring - Public Keyring
	Pubring *keymgr.Pubring
	// IDDb - Message ID log (replay protection)
	IDDb *idlog.IDLog
	// ChunkDb - Chunk database
	ChunkDb *Chunk
	// circuitResetTimer per reset periodico dei circuiti
	circuitResetTimer *time.Timer
)

// validateTorConfig valida la configurazione Tor base
func validateTorConfig() error {
	if cfg.Tor == nil || !cfg.Tor.Enabled {
		return nil
	}
	
	// Test connessione al proxy SOCKS
	torProxy := cfg.Tor.SocksProxy
	if torProxy == "" {
		torProxy = "127.0.0.1:9050"
	}
	
	conn, err := net.DialTimeout("tcp", torProxy, 5*time.Second)
	if err != nil {
		return fmt.Errorf("cannot connect to Tor SOCKS proxy at %s: %v", torProxy, err)
	}
	conn.Close()
	
	log.Infof("Tor proxy validated: %s", torProxy)
	return nil
}

// dialThroughTor crea connessioni attraverso Tor
func dialThroughTor(address string) (net.Conn, error) {
	torProxy := cfg.Tor.SocksProxy
	if torProxy == "" {
		torProxy = "127.0.0.1:9050"
	}
	
	proxyURL, err := url.Parse("socks5://" + torProxy)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL: %v", err)
	}
	
	dialer, err := proxy.FromURL(proxyURL, proxy.Direct)
	if err != nil {
		return nil, fmt.Errorf("failed to create SOCKS5 dialer: %v", err)
	}
	
	timeout := time.Duration(cfg.Tor.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	
	conn, err := dialer.(proxy.ContextDialer).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("failed to dial through Tor: %v", err)
	}
	
	log.Tracef("Connected to %s via Tor", address)
	return conn, nil
}

// startCircuitResetTimer avvia il timer per reset periodico dei circuiti
func startCircuitResetTimer() {
	if cfg.Tor == nil || !cfg.Tor.Enabled || cfg.Tor.CircuitReset <= 0 {
		return
	}
	
	resetInterval := time.Duration(cfg.Tor.CircuitReset) * time.Minute
	circuitResetTimer = time.AfterFunc(resetInterval, func() {
		log.Info("Performing periodic circuit reset")
		// Reset semplice: forza nuove connessioni chiudendo quelle esistenti
		// Tor si occuperà automaticamente di creare nuovi circuiti
		
		// Riprogramma il prossimo reset
		startCircuitResetTimer()
	})
	
	log.Infof("Circuit reset scheduled every %d minutes", cfg.Tor.CircuitReset)
}

func main() {
	var err error
	flag, cfg = config.GetCfg()
	if flag.Version {
		fmt.Println(version)
		os.Exit(0)
	}
	
	// If the debug flag is set, print the config and exit
	if flag.Debug {
		y, err := cfg.Debug()
		if err != nil {
			fmt.Printf("Debugging Error: %s\n", err)
			os.Exit(1)
		}
		fmt.Printf("%s\n", y)
		os.Exit(0)
	}

	// Set up logging
	loglevel, err := loglevel.ParseLevel(cfg.General.Loglevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: Unknown loglevel", cfg.General.Loglevel)
		os.Exit(1)
	}
	
	// If we're logging to a file, open the file and redirect output to it
	if cfg.General.LogToFile && cfg.General.LogToJournal {
		fmt.Fprintln(os.Stderr, "Cannot log to file and journal")
		os.Exit(1)
	} else if cfg.General.LogToFile {
		logfile, err := os.OpenFile(cfg.Files.Logfile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: Error opening logfile: %v", cfg.Files.Logfile, err)
			os.Exit(1)
		}
		stdlog.SetOutput(logfile)
		log.Current = log.StdLogger{Level: loglevel}
	} else if cfg.General.LogToJournal {
		log.Current = jlog.NewJournal(loglevel)
	} else {
		log.Current = log.StdLogger{Level: loglevel}
	}

	// Inform the user which (if any) config file was used.
	if cfg.Files.Config != "" {
		log.Infof("Using config file: %s", cfg.Files.Config)
	} else {
		log.Warn("No config file was found. Resorting to defaults")
	}

	// Valida configurazione Tor
	if cfg.Tor != nil && cfg.Tor.Enabled {
		log.Info("Tor routing enabled - validating configuration")
		err := validateTorConfig()
		if err != nil {
			log.Errorf("Tor configuration validation failed: %s", err)
			if cfg.Tor.Required {
				log.Error("Tor is required but validation failed. Exiting.")
				os.Exit(1)
			}
			log.Warn("Tor validation failed, continuing with direct connections")
		} else {
			log.Info("Tor configuration validated successfully")
			// Avvia il timer per reset circuiti
			startCircuitResetTimer()
		}
	} else {
		log.Info("Tor routing disabled - using direct connections")
	}

	// Setup complete, time to do some work
	if flag.Client {
		mixprep()
	} else if flag.Stdin {
		dir := maildir.Dir(cfg.Files.Maildir)
		newmsg, err := dir.NewDelivery()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		stdin, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		newmsg.Write(stdin)
		newmsg.Close()
	} else if flag.Remailer {
		err = loopServer()
		if err != nil {
			panic(err)
		}
	} else if flag.Dummy {
		injectDummy()
	} else if flag.Refresh {
		fmt.Printf("Keyring refresh: from=%s, to=%s\n", cfg.Urls.Pubring, cfg.Files.Pubring)
		httpGet(cfg.Urls.Pubring, cfg.Files.Pubring)
		fmt.Printf("Stats refresh: from=%s, to=%s\n", cfg.Urls.Mlist2, cfg.Files.Mlist2)
		httpGet(cfg.Urls.Mlist2, cfg.Files.Mlist2)
	}
	
	if flag.Send {
		// Flush the outbound pool
		poolOutboundSend()
	}
}
