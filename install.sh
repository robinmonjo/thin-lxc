#!/bin/bash

VERSION=0.3

echo "Downloading sources"
curl -sL https://github.com/robinmonjo/thin-lxc/archive/v$VERSION.tar.gz | tar -C /tmp -zxf -

echo "Building ..."
go build /tmp/thin-lxc-0.2/thin-lxc.go
sudo mv thin-lxc /usr/local/bin

echo "Cleaning up ..."
rm -rf /tmp/thin-lxc*

echo "Done"