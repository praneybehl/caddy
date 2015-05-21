package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"

	"github.com/mholt/caddy/app"
	"github.com/mholt/caddy/config"
	"github.com/mholt/caddy/server"
)

var (
	conf    string
	cpu     string
	version bool
)

func init() {
	flag.StringVar(&conf, "conf", "", "Configuration file to use (default="+config.DefaultConfigFile+")")
	flag.BoolVar(&app.Http2, "http2", true, "Enable HTTP/2 support") // TODO: temporary flag until http2 merged into std lib
	flag.BoolVar(&app.Quiet, "quiet", false, "Quiet mode (no initialization output)")
	flag.StringVar(&cpu, "cpu", "100%", "CPU cap")
	flag.StringVar(&config.Root, "root", config.DefaultRoot, "Root path to default site")
	flag.StringVar(&config.Host, "host", config.DefaultHost, "Default host")
	flag.StringVar(&config.Port, "port", config.DefaultPort, "Default port")
	flag.BoolVar(&version, "version", false, "Show version")

	config.AppName = "Caddy"
	config.AppVersion = "0.6.0"
}

func main() {
	flag.Parse()

	if version {
		fmt.Printf("%s %s\n", config.AppName, config.AppVersion)
		os.Exit(0)
	}

	// Set CPU cap
	err := app.SetCPU(cpu)
	if err != nil {
		log.Fatal(err)
	}

	// Load config from file
	allConfigs, err := loadConfigs()
	if err != nil {
		log.Fatal(err)
	}

	// Group by address (virtual hosts)
	addresses, err := config.ArrangeBindings(allConfigs)
	if err != nil {
		log.Fatal(err)
	}

	// Start each server with its one or more configurations
	for addr, configs := range addresses {
		s, err := server.New(addr, configs, configs[0].TLS.Enabled)
		if err != nil {
			log.Fatal(err)
		}
		s.HTTP2 = app.Http2 // TODO: This setting is temporary
		app.Wg.Add(1)
		go func(s *server.Server) {
			defer app.Wg.Done()
			err := s.Start()
			if err != nil {
				log.Fatal(err) // kill whole process to avoid a half-alive zombie server
			}
		}(s)

		app.Servers = append(app.Servers, s)

		if !app.Quiet {
			for _, config := range configs {
				fmt.Println(config.Address())
			}
		}
	}


	app.Wg.Wait()
}

// loadConfigs loads configuration from a file or stdin (piped).
// Configuration is obtained from one of three sources, tried
// in this order: 1. -conf flag, 2. stdin, 3. Caddyfile.
// If none of those are available, a default configuration is
// loaded.
func loadConfigs() ([]server.Config, error) {
	// -conf flag
	if conf != "" {
		file, err := os.Open(conf)
		if err != nil {
			return []server.Config{}, err
		}
		defer file.Close()
		return config.Load(path.Base(conf), file)
	}

	// stdin
	fi, err := os.Stdin.Stat()
	if err == nil && fi.Mode()&os.ModeCharDevice == 0 {
		// Note that a non-nil error is not a problem. Windows
		// will not create a stdin if there is no pipe, which
		// produces an error when calling Stat(). But Unix will
		// make one either way, which is why we also check that
		// bitmask.
		confBody, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			log.Fatal(err)
		}
		if len(confBody) > 0 {
			return config.Load("stdin", bytes.NewReader(confBody))
		}
	}

	// Caddyfile
	file, err := os.Open(config.DefaultConfigFile)
	if err != nil {
		if os.IsNotExist(err) {
			return []server.Config{config.Default()}, nil
		}
		return []server.Config{}, err
	}
	defer file.Close()

	return config.Load(config.DefaultConfigFile, file)
}
