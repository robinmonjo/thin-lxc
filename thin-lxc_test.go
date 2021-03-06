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

const HOST_MNT_FOLDER = "/tmp/thin-lxc-test"
const HOST_MNT_FILE = "/tmp/thin-lxc-test.conf"

const CONT_MNT_FOLDER = "/tmp/test"
const CONT_MNT_FILE = "/tmp/test.conf"

/*
Test bench
*/

var c1, c2, c3, c4 *Container
var containers []Container

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
	if err := downloadBaseCN(); err != nil {
		failTest(t, "Something went wrong while downloading base container", err)
	}

	os.MkdirAll(HOST_MNT_FOLDER, 0700)
	file, _ := os.Create(HOST_MNT_FILE)
	file.Close()

	c1, _ = newContainer(BASE_CONT_PATH, "thin-lxc-test-c11", "", "hostnname-c1", "10.0.3.245", "")
	c2, _ = newContainer(BASE_CONT_PATH, "thin-lxc-test-c12", "9999:8888", "hostname-c2", "10.0.3.246", "")
	c3, _ = newContainer(BASE_CONT_PATH, "thin-lxc-test-c13", "3666:8889", "hostname-c3", "10.0.3.247", HOST_MNT_FOLDER + ":" + CONT_MNT_FOLDER + "," + HOST_MNT_FILE + ":" + CONT_MNT_FILE)
	c4, _ = newContainer(BASE_CONT_PATH, "thin-lxc-test-c14", "", "", "", "")

	containers = []Container{*c1, *c2, *c3, *c4}

	rand.Seed(time.Now().Unix())
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
		nc, _ := unmarshall(c.Name)
		eq := reflect.DeepEqual(c, *nc)
		c1.cleanupFS()
		if eq == false {
			failTest(t, "marshalling/unmarshalling failed")
		}
	}
	fmt.Println("OK")
}

func Test_overlayfsMount(t *testing.T) {
	fmt.Print("Testing overlayfs mount/umount ... ")
	for i := range containers {
		c := containers[i]
		c.setupOnFS()
		if c.isMounted() {
			failTest(t, "overlayfs mount already performed ?")
		}
		c.overlayfsMount()
		if c.isMounted() == false {
			failTest(t, "overlayfs mount failed")
		}
		c.overlayfsUnmount(1)
		if c.isMounted() {
			failTest(t, "overlayfs unmount failed")
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
		c.overlayfsUnmount(5)
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


