#!/bin/bash

set -e

VERSION=0.4

echo "Downloading sources"
curl -sL https://github.com/robinmonjo/thin-lxc/archive/v$VERSION.tar.gz | tar -C /tmp -zxf - &> /dev/null

echo "Building ..."
go build /tmp/thin-lxc-$VERSION/thin-lxc.go
sudo mv thin-lxc /usr/local/bin

echo "Cleaning up ..."
rm -rf /tmp/thin-lxc*

echo "Done"