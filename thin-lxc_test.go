package main

import(
	"testing"
	"reflect"
	"os"
	"os/exec"
	"time"
)

const BASE_CONT_PATH = "/var/lib/lxc/baseCN"

const ID = "thin_lxc_test_id3"
const IP = "10.0.3.147"
const NAME = "thin_lxc_test_name3"

const HOST_MNT_FOLDER = "/tmp/thin-lxc-test"
const HOST_MNT_FILE = "/tmp/thin-lxc-test.conf"

/*
Test bench
*/

var c1 Container = Container {
	BASE_CONT_PATH,
	CONTAINERS_ROOT_PATH + "/" + ID,

	CONTAINERS_ROOT_PATH + "/" + ID + "/image",
	CONTAINERS_ROOT_PATH + "/" + ID + "/wlayer",
	CONTAINERS_ROOT_PATH + "/" + ID + "/image/rootfs",
	CONTAINERS_ROOT_PATH + "/" + ID + "/image/config",

	ID,
	IP,
	randomHwaddr(),
	NAME,

	0,
	0,

	map[string]string{},
}

var c2 Container
var c3 Container

var containers []Container

/*
Test bench setup
*/

func Test_init(t *testing.T) {
	c2 = c1
	c2.Port = 9999
	c2.HostPort = 8888

	c3 = c2
	c3.BindMounts = map[string]string {
									HOST_MNT_FOLDER:"/tmp/test",
									HOST_MNT_FILE:"/tmp/test.conf",
								}

	containers = []Container{c1, c2, c3}
}

/*
Individual method test
*/

func Test_parsePortsArg(t *testing.T) {
	hostPort, port := parsePortsArg("1800:6777")
	if hostPort != 1800 || port != 6777 {
		t.Error("ports parsing failed")
	}
	hostPort, port = parsePortsArg("hello there")
	if hostPort != 0 || port != 0 {
		t.Error("ports parsing failed")
	}
}

func Test_parseBindMountsArg(t *testing.T) {
	mounts := parseBindMountsArg("/tmp:/tmp/tmp")
	if mounts["/tmp"] != "/tmp/tmp" {
		t.Error("error parsing bind mounts")
	}
}

func Test_marshalling(t *testing.T) {
	for i := range containers {
		c := containers[i]
		c.setupOnFS()
		c.marshall()
		nc := unmarshall(ID)
		eq := reflect.DeepEqual(c, nc)
		c1.cleanupFS()
		if eq == false {
			t.Error("marshalling/unmarshalling failed")
		}
	}
}

func Test_aufsMount(t *testing.T) {
	for i := range containers {
		c := containers[i]
		c.setupOnFS()
		if c.isMounted() {
			t.Error("aufs mount already performed ?")
		}
		c.aufsMount()
		if c.isMounted() == false {
			t.Error("aufs mount failed")
		}
		c.aufsUnmount(1)
		if c.isMounted() {
			t.Error("aufs unmount failed")
		}
		c.cleanupFS()
	}
}

func Test_portForwarding(t *testing.T) {
	for i := range containers {
		c := containers[i]
		if c.iptablesRuleExists() {
			t.Error("iptables rule already exists")	
		}
		c.forwardPort()
		if c.iptablesRuleExists() == false && (c.Port != 0 || c.HostPort != 0) {
			t.Error("failed to add iptables rule")	
		}
		c.unforwardPort()
		if c.iptablesRuleExists() {
			t.Error("failed to remove iptables rule")	
		}
	}
}

/*
Scenarios test
*/

func Test_create(t *testing.T) {
	os.Mkdir(HOST_MNT_FOLDER, 0700)
	os.Create(HOST_MNT_FILE)

	for i := range containers {
		c := containers[i]
		c.create()
		if err := exec.Command("lxc-start", "-n", c.Name, "-f", c.ConfigPath, "-d").Run(); err != nil {
			t.Error("Unable to start container", err)
		}
		time.Sleep(5 * time.Second)
		if err := exec.Command("lxc-stop", "-n", c.Name).Run(); err != nil {
			t.Error("Unable to stop container", err)
		}
		c.destroy()
	}

	os.Remove(HOST_MNT_FOLDER)
	os.Remove(HOST_MNT_FILE)
}

