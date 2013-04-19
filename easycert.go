// Copyright 2013 Jonas mg
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package main

import (
	"errors"
	"flag"
	"fmt"
	"go/build"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"text/template"
	"time"
)

const (
	// Where the configuration template is installed through "go get".
	_DIR_CONFIG = "github.com/kless/easycert/data"

	_DIR_ROOT = ".cert" // Directory to store the certificates.
	_NAME_CA  = "ca"    // Name for files related to the CA.

	_FILE_CONFIG    = "openssl.cfg"
	_FILE_SERVER_GO = "z-cert_srv.go"
	_FILE_CLIENT_GO = "z-cert_cl.go"
)

// File extensions.
const (
	EXT_CERT  = ".crt" // Certificate (can be publicly distributed)
	EXT_KEY   = ".key" // Private key (with restrictive permissions)
	EXT_REVOK = ".crl" // Certificate Revokation List (can be publicly distributed)

	// Certificate Request (it will be signed by the CA in order to create the
	// server certificate. Afterwards it is not needed and can be deleted).
	EXT_REQUEST = ".csr"

	// For files that contain both the Key and the server certificate since some
	// servers need this. Permissions should be restrictive on these files.
	EXT_CERT_AND_KEY = ".pem"
)

// DirPath represents the directory structure.
type DirPath struct {
	Root  string // Root directory with certificates.
	Cert  string // Where the server certificates are placed.
	Key   string // Where the private keys are placed.
	Revok string // Where the certificate revokation list is placed.

	// Where OpenSSL puts the created certificates in PEM (unencrypted) format
	// and in the form 'cert_serial_number.pem' (e.g. '07.pem')
	NewCert string
}

// FilePath represents the files structure.
type FilePath struct {
	Cmd    string // OpenSSL' path
	Config string // OpenSSL configuration file.
	Index  string // Serves as a database for OpenSSL.
	Serial string // Contains the next certificate’s serial number.

	Cert    string // Certificate.
	Key     string // Private key.
	Request string // Certificate request.
}

var (
	Dir  *DirPath
	File *FilePath
)

// Set the directory structure.
func init() {
	log.SetFlags(0)
	log.SetPrefix("FAIL! ")

	cmdPath, err := exec.LookPath("openssl")
	if err != nil {
		log.Fatal("OpenSSL is not installed")
	}

	user, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}

	root := filepath.Join(user.HomeDir, _DIR_ROOT)

	Dir = &DirPath{
		Root:    root,
		Cert:    filepath.Join(root, "certs"),
		NewCert: filepath.Join(root, "newcerts"),
		Key:     filepath.Join(root, "private"),
		Revok:   filepath.Join(root, "crl"),
	}

	File = &FilePath{
		Cmd:    cmdPath,
		Config: filepath.Join(Dir.Root, _FILE_CONFIG),
		Index:  filepath.Join(Dir.Root, "index.txt"),
		Serial: filepath.Join(Dir.Root, "serial"),
	}
}

// == Flags

var (
	errMinSize = errors.New("key size must be at least of 2048")
	errSize    = errors.New("key size must be multiple of 1024")
)

// rsaSizeT represents the size in bits of RSA key to generate.
type rsaSizeT int

func (s *rsaSizeT) Set(value string) error {
	i, err := strconv.Atoi(value)
	if err != nil {
		return err
	}

	if i < 2048 {
		return errMinSize
	}
	if i%1024 != 0 {
		return errSize
	}
	*s = rsaSizeT(i)
	return nil
}

func (s *rsaSizeT) String() string {
	return strconv.Itoa(int(*s))
}

var (
	IsNew = flag.Bool("new", false, "create the directory structure to handle the certificates")
	IsCA  = flag.Bool("ca", false, "create the certification authority")

	RSASize rsaSizeT = 2048 // default
	Years            = flag.Int("years", 1,
		"number of years a certificate generated is valid;\n\twith `-ca` flag, the default is 10 years")

	IsRequest = flag.Bool("req", false, "create a certificate request")
	IsSignReq = flag.Bool("sign", false, "sign a certificate request")
	Host      = flag.String("host", "", "comma-separated hostnames and IPs to generate a certificate for")

	IsGoLang   = flag.Bool("lang-go", false, "generate files in Go language to handle some certificate")
	CACert     = flag.String("ca-cert", _NAME_CA, "name or file of CA's certificate")
	ServerCert = flag.String("server-cert", "", "name of server's certificate")

	IsCheck = flag.Bool("chk", false, "checking")
	IsCert  = flag.Bool("cert", false, "the file is a certificate")
	IsKey   = flag.Bool("key", false, "the file is a private key")

	IsCat      = flag.Bool("cat", false, "show certificate or key")
	IsInfo     = flag.Bool("i", false, "print out information of the certificate")
	IsEndDate  = flag.Bool("end-date", false, "print the date until it is valid")
	IsHash     = flag.Bool("hash", false, "print the hash value")
	IsFullInfo = flag.Bool("full", false, "print extensive information")
	IsIssuer   = flag.Bool("issuer", false, "print the issuer")
	IsName     = flag.Bool("name", false, "print the subject")

	IsCertList = flag.Bool("lc", false, "list the certificates built")
	IsReqList  = flag.Bool("lr", false, "list the request certificates built")
)

func init() {
	flag.Var(&RSASize, "rsa-size", "size in bits for the RSA key")
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: easycert FLAG... [NAME|FILENAME]

NOTE: FILENAME is the path of a certificate file, while NAME is the name
of a file to look for in the certificates directory.

* Directory:
  -new [-ca -rsa-size -years]

* Certificate requests:
  -req [-rsa-size -years] [-sign] [-host] NAME
  -sign NAME

* Create files for some language:
  -lang-go -server-cert [-ca-cert]

* ChecK:
  -chk [-cert|-key] NAME|FILENAME

* Information:
  -cat [-cert|-key] NAME|FILENAME
  -i [-end-date -hash -issuer -name] NAME|FILENAME
  -i -full NAME|FILENAME

* List:
  -lc -lr

The flags are:
`)

	flag.PrintDefaults()
	//os.Exit(2)
}

func main() {
	flag.Usage = usage
	flag.Parse()

	if flag.NFlag() == 0 {
		fmt.Fprintf(os.Stderr, "Generate and handle certificates\n")
		os.Exit(2)
	}

	isExit := false

	if len(flag.Args()) == 0 {
		if *IsCertList {
			match, err := filepath.Glob(filepath.Join(Dir.Cert, "*"+EXT_CERT))
			if err != nil {
				log.Fatal(err)
			}
			printCert(match)
			isExit = true
		}
		if *IsReqList {
			match, err := filepath.Glob(filepath.Join(Dir.Root, "*"+EXT_REQUEST))
			if err != nil {
				log.Fatal(err)
			}
			printCert(match)
			os.Exit(0)
		}
		if isExit {
			os.Exit(0)
		}
	}

	filename := ""

	// Set absolute paths.
	switch {
	case *IsRequest, *IsSignReq, *IsCA:
		if *IsCA {
			filename = _NAME_CA
		} else {
			filename = flag.Args()[0]
		}
		File.Cert = filepath.Join(Dir.Cert, filename+EXT_CERT)
		File.Key = filepath.Join(Dir.Key, filename+EXT_KEY)
		File.Request = filepath.Join(Dir.Root, filename+EXT_REQUEST)

	case *IsGoLang:
		if *CACert == "" || *ServerCert == "" {
			log.Fatal("Missing required parameter in `-ca-cert` or `-server-cert` flag")
		}
		if (*CACert)[0] != '.' && (*CACert)[0] != os.PathSeparator {
			*CACert = filepath.Join(Dir.Cert, *CACert+EXT_CERT)
		}
		File.Cert = filepath.Join(Dir.Cert, *ServerCert+EXT_CERT)
		File.Key = filepath.Join(Dir.Key, *ServerCert+EXT_KEY)

	case *IsNew:

	default:
		filename = flag.Args()[0]

		if filename[0] != '.' && filename[0] != os.PathSeparator {
			if *IsCert {
				filename = filepath.Join(Dir.Cert, filename+EXT_CERT)
			} else if *IsKey {
				filename = filepath.Join(Dir.Key, filename+EXT_KEY)
			}
		}
	}

	if *IsCheck {
		if *IsCert {
			CheckCert(filename)
		} else if *IsKey {
			CheckKey(filename)
		}
		os.Exit(0)
	}

	if *IsCat {
		if *IsCert {
			fmt.Print(InfoCert(filename))
		} else if *IsKey {
			fmt.Print(InfoKey(filename))
		}
		os.Exit(0)
	}

	if *IsInfo {
		if *IsFullInfo {
			fmt.Print(InfoFull(filename))
			os.Exit(0)
		}

		if *IsEndDate {
			fmt.Print(InfoEndDate(filename))
		}
		if *IsHash {
			fmt.Print(HashInfo(filename))
		}
		if *IsIssuer {
			fmt.Print(InfoIssuer(filename))
		}
		if *IsName {
			fmt.Print(InfoName(filename))
		}
		os.Exit(0)
	}

	if *IsRequest {
		if _, err := os.Stat(File.Request); !os.IsNotExist(err) {
			log.Fatalf("Certificate request already exists: %q", File.Request)
		}
		NewRequest()
		isExit = true
	}
	if *IsSignReq {
		if _, err := os.Stat(File.Cert); !os.IsNotExist(err) {
			log.Fatalf("Certificate already exists: %q", File.Cert)
		}
		if isExit {
			fmt.Print("\n== Sign\n\n")
		}
		SignReq()
		os.Exit(0)
	}
	if isExit {
		os.Exit(0)
	}

	if *IsGoLang {
		for _, v := range []string{_FILE_SERVER_GO, _FILE_CLIENT_GO} {
			if _, err := os.Stat(v); !os.IsNotExist(err) {
				log.Fatalf("File already exists: %q", v)
			}
		}
		Cert2Lang()
		os.Exit(0)
	}

	if *IsNew {
		if _, err := os.Stat(Dir.Root); !os.IsNotExist(err) {
			log.Fatalf("The directory structure exists: %q", Dir.Root)
		}
		SetupDir()
	}
	if *IsCA {
		*Years = 10
		BuildCA()
	}
}

// SetupDir creates the directory structure.
func SetupDir() {
	var err error

	for _, v := range []string{Dir.Root, Dir.Cert, Dir.NewCert, Dir.Key, Dir.Revok} {
		if err = os.Mkdir(v, 0755); err != nil {
			log.Fatal(err)
		}
	}
	if err = os.Chmod(Dir.Key, 0710); err != nil {
		log.Fatal(err)
	}

	file, err := os.Create(File.Index)
	if err != nil {
		log.Fatal(err)
	}
	file.Close()

	file, err = os.Create(File.Serial)
	if err != nil {
		log.Fatal(err)
	}
	_, err = file.Write([]byte{'0', '1', '\n'})
	file.Close()
	if err != nil {
		log.Fatal(err)
	}

	// Configuration template

	host, err := os.Hostname()
	if err != nil {
		log.Fatalf("Could not get hostname: %s\n\n"+
			"You may want to fix your '/etc/hosts' and/or DNS setup",
			err)
	}

	pkg, err := build.Import(_DIR_CONFIG, build.Default.GOPATH, build.FindOnly)
	if err != nil {
		log.Fatal("Data directory not found\n", err)
	}

	configTemplate := filepath.Join(pkg.Dir, _FILE_CONFIG+".tmpl")
	if _, err = os.Stat(configTemplate); os.IsNotExist(err) {
		log.Fatalf("Configuration template not found: %q", configTemplate)
	}

	tmpl, err := template.ParseFiles(configTemplate)
	if err != nil {
		log.Fatal("Parsing error in configuration: ", err)
	}

	tmpConfigFile, err := os.Create(File.Config)
	if err != nil {
		log.Fatal(err)
	}

	data := struct {
		RootDir  string
		HostName string
		AltNames string
	}{
		Dir.Root,
		host,
		"IP.1 = 127.0.0.1",
	}
	err = tmpl.Execute(tmpConfigFile, data)
	tmpConfigFile.Close()
	if err != nil {
		log.Fatal(err)
	}

	if err = os.Chmod(File.Config, 0600); err != nil {
		log.Print(err)
	}

	fmt.Printf("* Directory structure created in %q\n", Dir.Root)
}

// Cert2Lang creates files in language Go to handle the certificate.
func Cert2Lang() {
	CACertBlock, err := ioutil.ReadFile(*CACert)
	if err != nil {
		log.Fatal(err)
	}
	CertBlock, err := ioutil.ReadFile(File.Cert)
	if err != nil {
		log.Fatal(err)
	}
	KeyBlock, err := ioutil.ReadFile(File.Key)
	if err != nil {
		log.Fatal(err)
	}

	version, err := exec.Command(File.Cmd, "version").Output()
	if err != nil {
		log.Fatal(err)
	}

	// Server

	file, err := os.OpenFile(_FILE_SERVER_GO, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatal(err)
	}

	tmpl := template.Must(template.New("").Parse(TMPL_SERVER_GO))
	data := struct {
		System     string
		Arch       string
		Version    string
		Date       string
		ValidUntil string
		CACert     string
		Cert       string
		Key        string
	}{
		runtime.GOOS,
		runtime.GOARCH,
		strings.TrimRight(string(version), "\n"),
		time.Now().Format(time.RFC822),
		fmt.Sprint(strings.TrimRight(InfoEndDate(File.Cert), "\n")),
		GoBlock(CACertBlock).String(),
		GoBlock(CertBlock).String(),
		GoBlock(KeyBlock).String(),
	}

	err = tmpl.Execute(file, data)
	file.Close()
	if err != nil {
		log.Fatal(err)
	}

	// Client

	file, err = os.OpenFile(_FILE_CLIENT_GO, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatal(err)
	}

	tmpl = template.Must(template.New("").Parse(TMPL_CLIENT_GO))

	err = tmpl.Execute(file, data)
	file.Close()
	if err != nil {
		log.Fatal(err)
	}
}

// printCert prints the name of the certificates.
func printCert(cert []string) {
	if len(cert) == 0 {
		return
	}
	for i, v := range cert {
		if i != 0 {
			fmt.Print("\t")
		}
		fmt.Print(filepath.Base(v))
	}
	fmt.Println()
}
