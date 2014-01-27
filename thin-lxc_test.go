package main

import(
	"testing"
	"reflect"
	"os"
	"os/exec"
	"time"
	"fmt"
	"math/rand"
)

const BASE_CONT_PATH = "/var/lib/lxc/baseCN"

const ID_1 = "thin-lxc-test-c1"
const NAME_1 = "thin-lxc-test-name-c1"

const ID_2 = "thin-lxc-test-c2"
const NAME_2 = "thin-lxc-test-name-c2"

const ID_3 = "thin-lxc-test-c3"
const NAME_3 = "thin-lxc-test-name-c3"

const HOST_MNT_FOLDER = "/tmp/thin-lxc-test"
const HOST_MNT_FILE = "/tmp/thin-lxc-test.conf"

const CONT_MNT_FOLDER = "/tmp/test"
const CONT_MNT_FILE = "/tmp/test.conf"

/*
Test bench
*/

var c1 Container = Container {
	BASE_CONT_PATH,
	CONTAINERS_ROOT_PATH + "/" + ID_1,

	CONTAINERS_ROOT_PATH + "/" + ID_1 + "/" + NAME_1,
	CONTAINERS_ROOT_PATH + "/" + ID_1 + "/.wlayer",
	CONTAINERS_ROOT_PATH + "/" + ID_1 + "/" + NAME_1 + "/rootfs",
	CONTAINERS_ROOT_PATH + "/" + ID_1 + "/" + NAME_1 + "/config",

	ID_1,
	"10.0.3.245",
	randomHwaddr(),
	NAME_1,

	0,
	0,

	map[string]string{},
}

var c2 Container = Container {
	BASE_CONT_PATH,
	CONTAINERS_ROOT_PATH + "/" + ID_2,

	CONTAINERS_ROOT_PATH + "/" + ID_2 + "/" + NAME_2,
	CONTAINERS_ROOT_PATH + "/" + ID_2 + "/.wlayer",
	CONTAINERS_ROOT_PATH + "/" + ID_2 + "/" + NAME_2 + "/rootfs",
	CONTAINERS_ROOT_PATH + "/" + ID_2 + "/" + NAME_2 + "/config",

	ID_2,
	"10.0.3.246",
	randomHwaddr(),
	NAME_2,

	9999, 8888,

	map[string]string{},
}

var c3 Container = Container {
	BASE_CONT_PATH,
	CONTAINERS_ROOT_PATH + "/" + ID_3,

	CONTAINERS_ROOT_PATH + "/" + ID_3 + "/" + NAME_3,
	CONTAINERS_ROOT_PATH + "/" + ID_3 + "/.wlayer",
	CONTAINERS_ROOT_PATH + "/" + ID_3 + "/" + NAME_3 + "/rootfs",
	CONTAINERS_ROOT_PATH + "/" + ID_3 + "/" + NAME_3 + "/config",

	ID_3,
	"10.0.3.247",
	randomHwaddr(),
	NAME_3,

	3666, 8889,

	map[string]string { HOST_MNT_FOLDER: CONT_MNT_FOLDER, HOST_MNT_FILE: CONT_MNT_FILE, },
}

var containers []Container = []Container{c1, c2, c3}

/*
Failure
*/
func failTest(t *testing.T, m string, args ... interface{}) {
	cleanup()
	t.Fatal(m, args)
}

/*
Test bench setup
*/

func Test_init(t *testing.T) {
	os.MkdirAll(HOST_MNT_FOLDER, 0700)
	file, _ := os.Create(HOST_MNT_FILE)
	file.Close()
	rand.Seed(time.Now().Unix())
	if fileExists(BASE_CONT_PATH) == false {
		fmt.Println("Warning: a base container in /var/lib/lxc/baseCN should exists for tests to perform")
	}
}

/*
Individual method test
*/

func Test_parsePortsArg(t *testing.T) {
	fmt.Print("Testing port option parsing ... ")
	hostPort, port := parsePortsArg("1800:6777")
	if hostPort != 1800 || port != 6777 {
		failTest(t, "ports parsing failed")
	}
	hostPort, port = parsePortsArg("hello there")
	if hostPort != 0 || port != 0 {
		failTest(t, "ports parsing failed")
	}
	fmt.Println("OK")
}

func Test_parseBindMountsArg(t *testing.T) {
	fmt.Print("Testing bind mounts option parsing ... ")
	mounts := parseBindMountsArg("/tmp:/tmp/tmp")
	if mounts["/tmp"] != "/tmp/tmp" {
		failTest(t, "error parsing bind mounts")
	}
	fmt.Println("OK")
}

func Test_marshalling(t *testing.T) {
	fmt.Print("Testing container metadata marshalling/unmarshalling ... ")
	for i := range containers {
		c := containers[i]
		c.setupOnFS()
		c.marshall()
		nc, _ := unmarshall(c.Id)
		eq := reflect.DeepEqual(c, *nc)
		c1.cleanupFS()
		if eq == false {
			failTest(t, "marshalling/unmarshalling failed")
		}
	}
	fmt.Println("OK")
}

func Test_aufsMount(t *testing.T) {
	fmt.Print("Testing aufs mount/umount ... ")
	for i := range containers {
		c := containers[i]
		c.setupOnFS()
		if c.isMounted() {
			failTest(t, "aufs mount already performed ?")
		}
		c.aufsMount()
		if c.isMounted() == false {
			failTest(t, "aufs mount failed")
		}
		c.aufsUnmount(1)
		if c.isMounted() {
			failTest(t, "aufs unmount failed")
		}
		c.cleanupFS()
	}
	fmt.Println("OK")
}

func Test_portForwarding(t *testing.T) {
	fmt.Print("Testing iptables rules add/delete ... ")
	for i := range containers {
		c := containers[i]
		if c.iptablesRuleExists() {
			failTest(t, "iptables rule already exists")	
		}
		c.forwardPort()
		if c.iptablesRuleExists() == false && (c.Port != 0 || c.HostPort != 0) {
			failTest(t, "failed to add iptables rule")	
		}
		c.unforwardPort()
		if c.iptablesRuleExists() {
			failTest(t, "failed to remove iptables rule")	
		}
	}
	fmt.Println("OK")
}

func Test_containerStateMonitoring(t *testing.T) {
	fmt.Print("Testing state change monitoring ... ")
	c := containers[0]
	if err := c.create(); err != nil {
		failTest(t, "Failed to create container", err)
	}
	
	cs := make(chan string)
	go monitorContainerForState(&c, C_STOPPED, cs)
	state := <-cs
	if state != C_STOPPED {
		failTest(t, "Expected", C_STOPPED, "got", state)
	}

	cs = make(chan string)
	go monitorContainerForState(&c, C_RUNNING, cs)
	if err := c.start(); err != nil {
		failTest(t, "Failed to start container", err)
	}
	state = <-cs
	if state != C_RUNNING {
		failTest(t, "Excepted", C_RUNNING, "got", state)
	}

	cs = make(chan string)
	go monitorContainerForState(&c, C_STOPPED, cs)
	if err := c.stop(); err != nil {
		failTest(t, "Failed to stop container", err)
	}
	c.destroy()
	state = <-cs
	if state != C_STOPPED {
		failTest(t, "Excepted", C_STOPPED, "got", state)
	}
	fmt.Println("OK")
}

/*
Scenarios test
*/

func Test_create(t *testing.T) {
	fmt.Print("Testing full creation + sanity check ... ")
	for i := range containers {
		c := containers[i]

		//Creating
		c.create()
		if err := c.start(); err != nil {
			failTest(t, "Unable to start container", err)
		}
		cs := make(chan string)
		go monitorContainerForState(&c, C_RUNNING, cs)
		<-cs

		c.checkInternal(i == 2, t)
		
		//Cleaning up
		if err := c.stop(); err != nil {
			failTest(t, "Unable to stop container", err)
		}
		c.destroy()
		if fileExists(c.Path) {
			failTest(t, "Container not properly cleaned-up")
		}
	}
	fmt.Println("OK")
}

func Test_reload(t *testing.T) {
	fmt.Print("Testing reload ... ")
	for i := range containers {
		c := containers[i]
		if err := c.create(); err != nil {
			failTest(t, "Failed to create container", err)
		}
		if err := c.start(); err != nil {
			failTest(t, "Failed to start container", err)
		}
		cs := make(chan string)
		go monitorContainerForState(&c, C_RUNNING, cs)
		<-cs
	}

	for i := range containers {
		c := containers[i]
		c.checkInternal(i == 2, t)
	}

	//simulate shutdown
	for i := range containers {
		c := containers[i]
		if err := c.stop(); err != nil {
			failTest(t, "Failed to stop container", err)
		}
		c.aufsUnmount(5)
		c.unforwardPort()
	}

	reload()

	//check if all is back in place and cleanup
	for i := range containers {
		c := containers[i]
		if err := c.start(); err != nil {
			failTest(t, "Failed to restart container after reload", err)
		}
		cs := make(chan string)
		go monitorContainerForState(&c, C_RUNNING, cs)
		<-cs

		c.checkInternal(i == 2, t)

		if c.iptablesRuleExists() == false && (c.Port != 0 || c.HostPort != 0) {
			failTest(t, "Failed to setup iptables rules after reload")	
		}

		if err := c.stop(); err != nil {
			failTest(t, "Failed to stop container", err)
		}
		c.destroy()
	}
	fmt.Println("OK")
}

/*
	Helper
*/
func (c *Container) start() error {
	return runCmdWithDetailedError(exec.Command("lxc-start", "-n", c.Name, "-f", c.ConfigPath, "-d"))
}

func (c *Container) stop() error {
	return runCmdWithDetailedError(exec.Command("lxc-stop", "-n", c.Name))
}

func (c *Container) checkInternal(testBindMount bool, t *testing.T) {
	if c.isRunning() == false {
		failTest(t, c.Name, "Apperas not to be running")
	}
	//test network
	if err := runCmdWithDetailedError(exec.Command("lxc-attach", "-n", c.Name, "--", "/bin/ping", "-c", "3", "www.google.com")); err != nil {
		failTest(t, "Unable to ping google", err)
	}
	//test bind mounts
	if testBindMount == false {
		return
	}
	if err := runCmdWithDetailedError(exec.Command("lxc-attach", "-n", c.Name, "--", "/usr/bin/test", "-d", CONT_MNT_FOLDER)); err != nil {
		failTest(t, "Bind mount folder failed", err)
	}
	if err := runCmdWithDetailedError(exec.Command("lxc-attach", "-n", c.Name, "--", "/usr/bin/test", "-f", CONT_MNT_FILE)); err != nil {
		failTest(t, "Bind mount file failed", err)
	}
}

/*
	Cleanup tests
*/
func Test_cleanup(t *testing.T) {
	os.Remove(HOST_MNT_FOLDER)
	os.Remove(HOST_MNT_FILE)
}

func cleanup() {
	fmt.Print("Failed, cleaning up ... ")
	for i := range containers {
		c := containers[i]
		c.stop();
		cs := make(chan string)
		go monitorContainerForState(&c, C_STOPPED, cs)
		<-cs
		c.destroy() //destroy ignoring errors
	}
	fmt.Println("OK")
}


