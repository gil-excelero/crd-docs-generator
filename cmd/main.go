package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/pkg/errors"
	"k8s.io/klog"
)

var (
	flAPIDir      = flag.String("api-dir", "", "api directory (or import path), point this to pkg/apis")
	flConfig      = flag.String("config", "config/config.json", "path to config file")
	flTemplateDir = flag.String("template-dir", "templates/html", "path to template/ dir")

	flHTTPAddr = flag.String("http-addr", "", "start an HTTP server on specified addr to view the result (e.g. :8080)")
	flOutFile  = flag.String("out-file", "", "path to output file to save the result")
)

func initFlags() {
	klog.InitFlags(nil)
	flag.Set("alsologtostderr", "true") // for klog
	flag.Parse()

	if *flConfig == "" {
		panic("-config not specified")
	}
	if *flAPIDir == "" {
		panic("-api-dir not specified")
	}
	if *flHTTPAddr == "" && *flOutFile == "" {
		panic("-out-file or -http-addr must be specified")
	}
	if *flHTTPAddr != "" && *flOutFile != "" {
		panic("only -out-file or -http-addr can be specified")
	}

	if err := isDirExists(*flTemplateDir); err != nil {
		panic(err)
	}

	if err := isDirExists(*flAPIDir); err != nil {
		panic(err)
	}
}

func isDirExists(dir string) error {
	path, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(path); err != nil {
		return errors.Wrapf(err, "cannot read the %s directory", path)
	} else if !fi.IsDir() {
		return errors.Errorf("%s path is not a directory", path)
	}
	return nil
}

func readConfigFromFile() GeneratorConfig {
	f, err := os.Open(*flConfig)
	if err != nil {
		klog.Fatalf("failed to open config file: %+v", err)
	}
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	var config GeneratorConfig
	if err := d.Decode(&config); err != nil {
		klog.Fatalf("failed to parse config file: %+v", err)
	}

	return config
}

func main() {
	defer klog.Flush()

	initFlags()

	config := readConfigFromFile()

	klog.V(3).Infof("log level 4+")

	klog.Infof("parsing go packages in directory %s", *flAPIDir)

	pkgs, err := ParseAPIPackages(*flAPIDir)
	if err != nil {
		klog.Fatal(err)
	}
	if len(pkgs) == 0 {
		klog.Fatalf("no API packages found in %s", *flAPIDir)
	}

	apiPackages, err := combineAPIPackages(pkgs)
	if err != nil {
		klog.Fatal(err)
	}

	s, err := generateDoc(apiPackages, config)
	if err != nil {
		klog.Fatalf("failed: %+v", err)
	}

	if *flOutFile != "" {
		outputToFile(s)
	}

	if *flHTTPAddr != "" {

	}
}

func outputToFile(s string) {
	dir := filepath.Dir(*flOutFile)

	if err := os.MkdirAll(dir, 0755); err != nil {
		klog.Fatalf("failed to create dir %s: %v", dir, err)
	}

	if err := ioutil.WriteFile(*flOutFile, []byte(s), 0644); err != nil {
		klog.Fatalf("failed to write to out file: %v", err)
	}

	klog.Infof("written to %s", *flOutFile)
}

func serverWithHttpServer(s string) {
	h := func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		defer func() { klog.Infof("request took %v", time.Since(now)) }()

		if _, err := fmt.Fprint(w, s); err != nil {
			klog.Warningf("response write error: %v", err)
		}
	}
	http.HandleFunc("/", h)
	klog.Infof("server listening at %s", *flHTTPAddr)
	klog.Fatal(http.ListenAndServe(*flHTTPAddr, nil))
}

func generateDoc(apiPackages []*apiPackage, config GeneratorConfig) (string, error) {
	var b bytes.Buffer
	err := Render(&b, apiPackages, config)
	if err != nil {
		return "", errors.Wrap(err, "failed to render the result")
	}

	if config.PreserveTrailingWhitespace {
		return b.String(), nil
	}

	// remove trailing whitespace from each html line for markdown renderers
	s := regexp.MustCompile(`(?m)^\s+`).ReplaceAllString(b.String(), "")
	return s, nil
}
