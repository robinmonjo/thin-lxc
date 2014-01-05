package main

import(
	"testing"
	"reflect"
)

const ID = "00112233445566778899"
const IP = "10.0.3.146"
const NAME = "test"
const HOST_PORT = 3000
const CONT_PORT = 3200
const BASE_CONT_PATH = "/var/lib/lxc/baseCN"
const HWADDR = "00:11:22:33:44"

var c Container = Container {
	BASE_CONT_PATH,
	CONTAINERS_ROOT_PATH + "/" + ID,

	CONTAINERS_ROOT_PATH + "/" + ID + "/image",
	CONTAINERS_ROOT_PATH + "/" + ID + "/wlayer",
	CONTAINERS_ROOT_PATH + "/" + ID + "/image/rootfs",
	CONTAINERS_ROOT_PATH + "/" + ID + "/image/rootfs/config",

	ID,
	IP,
	HWADDR,
	NAME,

	CONT_PORT,
	HOST_PORT,
	
	map[string]string{
			"/var/log": "/etc/lol",
			"/var/log1": "/etc/lol1",
	},
}

func Test_marshalling(t *testing.T) {
	c.setupOnFS()
	c.marshall()
	loadedC := unmarshall(ID)
	eq := reflect.DeepEqual(c, loadedC)
	if eq == false {
		t.Error("marshalling/unmarshalling failed")
	}
}

func Test_aufsMount(t *testing.T) {
	if fileExists(c.Rootfs) {
		t.Error("aufs mount already performed ?")
	}
	c.aufsMount()
	if fileExists(c.Rootfs) == false {
		t.Error("aufs mount failed")
	}
	c.aufsUnmount()
	if fileExists(c.Rootfs) {
		t.Error("aufs unmount failed")
	}
}

func Test_isRunning(t *testing.T) {
	if c.isRunning() {
		t.Error("indicate running while it's false")	
	}
}

func Test_portForwarding(t *testing.T) {
	c.forwardPort(true)
	c.forwardPort(false)
	//TODO check that the rule is actually added and removed
}

func Test_createConfig(t *testing.T) {
	c.createConfig()
}

