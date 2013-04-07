EasyCert
========
Wrap over OpenSSL to create and handle certificates.

[Documentation online](http://godoc.org/github.com/kless/easycert)

## Installation

	go get github.com/kless/easycert

## Usage

To generate a certificate for Go language:

	easycert -lang-go

To create a certificate in '/etc/ssl':

	sudo env PATH=$PATH GOPATH=$GOPATH easycert -root

## License

The source files are distributed under the [Mozilla Public License, version 2.0](http://mozilla.org/MPL/2.0/),
unless otherwise noted.  
Please read the [FAQ](http://www.mozilla.org/MPL/2.0/FAQ.html)
if you have further questions regarding the license.

* * *
*Generated by [Gowizard](https://github.com/kless/wizard)*
