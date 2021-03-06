package main

import(
	"flag"
	"fmt"
	"log"
	"encoding/json"
	"io/ioutil"
	"os/exec"
	"os"
	"strconv"
	"strings"
	"text/template"
	"math/rand"
	"time"
	"path"
	"errors"
	"net/http"
	"io"
	"crypto/md5"
)

const VERSION = "0.4"
const CONTAINERS_ROOT_PATH = "/containers"

/*
LXC container state as returned by the lxc-info command
*/
const (
	C_STARTING = "STARTING"
	C_RUNNING = "RUNNING"
	C_STOPPING = "STOPPING"
	C_STOPPED = "STOPPED"
	C_UNKNOWN = "UNKNOWN"
)

const BASE_CN_URL = "https://s3-eu-west-1.amazonaws.com/thin-lxc/baseCN.tar.gz"
const BASE_CN_PATH = "/var/lib/lxc"
const BASE_CN_MD5_URL = "https://s3-eu-west-1.amazonaws.com/thin-lxc/md5-baseCN.txt"

/*
Flag parsing
*/

var vFlag = flag.Bool("v", false, "print version and exit")

var aFlag = flag.String("a", "", "action to perform")
var nFlag = flag.String("n", "", "name of the container")
var hnFlag = flag.String("hn", "", "hostname of the container (hostname == name if name is nil)")
var ipFlag = flag.String("ip", "", "ip of the container")
var bFlag = flag.String("b", "/var/lib/lxc/baseCN", "path to the base container rootfs")
var pFlag = flag.String("p", "", "port to forward host_port:cont_port")
var mFlag = flag.String("m", "", "bind mount of type path_host:cont_host,...")

/*
Container type + methods
*/

type Container struct {
	BaseContainerPath string     
	Path string                  

	RoLayer string
	WrLayer string               
	Rootfs string
	ConfigPath string

	HostName string
	Ip string
	Inet string        //if ip, manual else dhcp
	Hwaddr string
	Name string

	Port int
	HostPort int

	BindMounts map[string]string
}

func newContainer(baseCn string, name string, ports string, hostName string, ip string, bindMounts string) (*Container, error) {
	path := CONTAINERS_ROOT_PATH + "/" + name
	if fileExists(path) {
		return nil, errors.New("Container with such name already exists")
	}
	hostPort, port := parsePortsArg(ports)

	//if ip is defined, use static, else dhcp
	inet := "dhcp"
	if len(ip) > 0 {
		inet = "manual"
	}

	//if no hostname provided, hostname == name
	if len(hostName) == 0 {
		hostName = name
	}

	c := &Container{
		baseCn,                      
		path,                            
    
		path + "/" + name,                
		path + "/.wlayer",               
		path + "/" + name + "/rootfs",   
		path + "/" + name + "/config",      

		hostName,                            
		ip,
		inet,
		randomHwaddr(),                
		name,                          
		port,                          
		hostPort,                      
		parseBindMountsArg(bindMounts),
	}
	return c, nil
}

func (c *Container) IpConfig() string {
	return c.Ip + "/24"
}

func (c *Container) FstabConfig() string {
	return c.RoLayer + "/" + "fstab"
}

func (c *Container) HasStaticIp() bool {
	return len(c.Ip) > 0
}

func (c *Container) setupOnFS() error {
	if err := os.MkdirAll(c.RoLayer, 0700); err != nil {
		return err
	}
	return os.MkdirAll(c.WrLayer, 0700)
}

func (c *Container) cleanupFS() error {
	return os.RemoveAll(c.Path)
}

func (c *Container) iptablesRuleDo(action string) error {
	cmd := exec.Command("iptables", "-t", "nat", action, "PREROUTING", "-p", "tcp", "!", "-s", "10.0.3.0/24", "--dport", strconv.Itoa(c.HostPort), "-j", "DNAT", "--to-destination", c.Ip + ":" + strconv.Itoa(c.Port))
	return runCmdWithDetailedError(cmd)
}

func (c *Container) iptablesRuleExists() bool {
	return c.iptablesRuleDo("-C") == nil
}

func (c *Container) forwardPort() error {
	if c.Port == 0 && c.HostPort == 0 {
		return nil
	}
	if c.iptablesRuleExists() {
		return errors.New("Trying to add iptables rule that already exists")
	}
	return c.iptablesRuleDo("-A")
}

func (c *Container) unforwardPort() error {
	if c.iptablesRuleExists() == false { //rule doesn't exists
		return nil
	}
	return c.iptablesRuleDo("-D")
}

func (c *Container) executeTemplate(content string, path string) error {
	tmpl, err := template.New("thin-lxc").Parse(content)
	if err != nil {
		return err
	}
	file, err := os.Create(path)
	defer file.Close()
	if err != nil {
		return err
	}
	return tmpl.Execute(file, c)
}

func (c *Container) configureFiles() error {
	configs := map[string]string{
		CONFIG_FILE: c.RoLayer + "/config",
		INTERFACES_FILE: c.Rootfs + "/etc/network/interfaces",
		HOSTS_FILE: c.Rootfs + "/etc/hosts",
		HOSTNAME_FILE: c.Rootfs + "/etc/hostname",
		SETUP_GATEWAY_FILE: c.Rootfs + "/etc/init/setup-gateway.conf",
	}
	for template, path := range configs {
		if err := c.executeTemplate(template, path); err != nil {
			return err
		}	
	}
	return nil
}

func (c *Container) overlayfsMount() error {
	mnt := "upperdir=" + c.WrLayer + ",lowerdir=" + c.BaseContainerPath
	return runCmdWithDetailedError(exec.Command("mount", "-t", "overlayfs", "-o", mnt, "none", c.RoLayer))
}

func (c *Container) overlayfsUnmount(tryCount int) error {
	if err := runCmdWithDetailedError(exec.Command("umount", c.RoLayer)); err != nil {
		if tryCount >= 0 {
			time.Sleep(1 * time.Second)
			c.overlayfsUnmount(tryCount - 1)
		} else {
			return err
		}
	}
	return nil
}

func (c *Container) isMounted() bool {
	return fileExists(c.Rootfs)
}

func (c *Container) prepareBindMounts() error {
	for hostMntPath, contMntPath := range c.BindMounts {
		if fileExists(hostMntPath) == false {
			return errors.New(hostMntPath + " doesn't exists")
		}
		if strings.HasPrefix(c.BindMounts[hostMntPath], c.Rootfs) == false {
			c.BindMounts[hostMntPath] = c.Rootfs + contMntPath
		}

		if fileExists(c.BindMounts[hostMntPath]) {
			continue
		}

		var err error
		if path.Ext(c.BindMounts[hostMntPath]) == "" { //no extensions considering it's a file
			err = os.MkdirAll(c.BindMounts[hostMntPath], 0700)	
		} else {
			err = os.MkdirAll(path.Dir(c.BindMounts[hostMntPath]), 0700)
			var file *os.File
			file, err = os.Create(c.BindMounts[hostMntPath])
			defer file.Close()
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Container) marshall() error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(c.Path + "/.metadata.json", b, 0644)
}

func (c *Container) state() string {
	stdout, err := exec.Command("lxc-info", "-n", c.Name).Output()
	if err != nil {
		log.Println("lxc-info failed", err)
		return C_UNKNOWN
	}
	return strings.Trim(strings.Split(strings.Split(string(stdout), "\n")[0], ":")[1], " ")
}

func (c *Container) isRunning() bool {
	return c.state() == C_RUNNING
}

func (c *Container) create() error {
	if err := c.setupOnFS(); err != nil {
		return err
	}
	if err := c.marshall(); err != nil {
		return err
	}
	if err := c.overlayfsMount(); err != nil {
		return err
	}
	if err := c.prepareBindMounts(); err != nil {
		return err
	}
	if err := c.configureFiles(); err != nil {
		return err
	}
	if err := c.forwardPort(); err != nil {
		return err
	}
	return nil
}

func (c *Container) destroy() error {
	if err := c.unforwardPort(); err != nil {
		return err
	}
	if err := c.overlayfsUnmount(5); err != nil {
		return err
	}
	return c.cleanupFS()
}

func (c *Container) reload() error {
	if c.isMounted() == false {
		if err := c.overlayfsMount(); err != nil {
			return err
		}
	}
	return c.forwardPort()
}

/*
Helper methods
*/
func unmarshall(name string) (*Container, error) {
	b, err := ioutil.ReadFile(CONTAINERS_ROOT_PATH + "/" + name + "/.metadata.json")
	if err != nil {
		return nil, err
	}
	var c Container
	if err = json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func randomHwaddr() string {
	arr := []string{"00:16:3e"}
	for i := 0; i < 3; i++ {
		arr = append(arr, fmt.Sprintf("%0.2x", rand.Intn(256)))
	}
	return strings.Join(arr, ":")
}

func parsePortsArg(ports string) (hostPort int, port int) {
	hostPort = 0
	port = 0
	var err error
	if len(ports) == 0 {
		return
	}
	hostPort, err = strconv.Atoi(strings.Split(ports, ":")[0])
	if err != nil {
		hostPort = 0
		return
	}
	port, err = strconv.Atoi(strings.Split(ports, ":")[1])
	if err != nil {
		port = 0;
		return
	}
	return
}

func parseBindMountsArg(mounts string) (bindMounts map[string]string) {
	bindMounts = make(map[string]string)
	if len(mounts) == 0 {
		return
	}
	arr := strings.Split(mounts, ",")
	for i := range arr {
		hostMountPath := strings.Split(arr[i], ":")[0]
		contMountPath := strings.Split(arr[i], ":")[1]
		bindMounts[hostMountPath] = contMountPath
	}
	return
}

func runCmdWithDetailedError(cmd *exec.Cmd) error {
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf(string(out), err)
	}
	return nil
}

/*
Usage:
cs := make(chan string)
go monitorContainerForState(c, C_RUNNING, cs)
state := <-cs
*/
func monitorContainerForState(c *Container, state string, cs chan string) {
	curState := c.state()
	if curState == state {
		if state == C_RUNNING {
			time.Sleep(5 * time.Second) //wait extra time to make sure network is up
		}
		cs <- curState
		close(cs)
		return
	}
	time.Sleep(500 * time.Millisecond)
	monitorContainerForState(c, state, cs)
}

func downloadFromUrl(url string, path string, fileName string) error {
	if  err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	output, err := os.Create(path + "/" + fileName)
	if err != nil {
		return err
	}
	defer output.Close()

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	_, err = io.Copy(output, resp.Body)
	return err
}

func downloadBaseCN() error {
	if fileExists(BASE_CN_PATH + "/baseCN/rootfs") {
		return nil
	}
	fmt.Print("First time thin-lxc, downloading base container ... ")
	//download tar
	if err := downloadFromUrl(BASE_CN_URL, BASE_CN_PATH, "baseCN.tar.gz"); err != nil {
		return err
	}
	fmt.Println("Done")
	fmt.Print("Checking base container integrity ... ")
	//check md5
	resp, err := http.Get(BASE_CN_MD5_URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	expectedSum, err := ioutil.ReadAll(resp.Body)
	f, err := os.Open(BASE_CN_PATH + "/baseCN.tar.gz")
	if err != nil {
		return err
	}
	defer f.Close()
	md5 := md5.New()
	io.Copy(md5, f)
	sum := fmt.Sprintf("%x", md5.Sum(nil))
	if strings.Replace(string(expectedSum), "\n", "", -1) != sum {
		return errors.New("MD5 sum check failed " + string(expectedSum) + " != " + sum)
	}
	fmt.Println("Done")

	//untar
	fmt.Print("Extracting base container to ", BASE_CN_PATH, " ... ")
	untar := exec.Command("sudo", "tar", "-C", BASE_CN_PATH, "-xf", BASE_CN_PATH + "/baseCN.tar.gz")
	err = runCmdWithDetailedError(untar)
	if err != nil {
		return err
	}
	fmt.Println("Done")
	return nil
}

/*
Action methods
*/

func create() {
	c, err := newContainer(*bFlag, *nFlag, *pFlag, *hnFlag, *ipFlag, *mFlag)
	if err != nil {
		log.Fatal(err)
	}
	if err := c.create(); err != nil {
		log.Fatal("Unable to create container", err)
	}

	fmt.Println("Container created start using: \"lxc-start -n", c.Name,"-f", c.ConfigPath, "-d\"")
}

func destroy() {
	c, err := unmarshall(*nFlag)
	if err != nil {
		log.Fatal("Unable to unmarchall container metadata", err)
	}
	if c.isRunning() {
		log.Fatal("Container is running. Stop it before destroying it")
	}
	if err := c.destroy(); err != nil {
		log.Fatal("Unable to destroy container", err)
	}
}

func reload() {
	//after a reboot, overlayfs mount and iptables rules will be deleted, reload will reset everything
	dirs, err := ioutil.ReadDir(CONTAINERS_ROOT_PATH)
	if err != nil {
		log.Fatal(err)
	}
	for i := range dirs {
		dir := dirs[i]
		if fileExists(CONTAINERS_ROOT_PATH + "/" + dir.Name() + "/.metadata.json") == false {
			continue
		}
		c, err := unmarshall(dir.Name())
		if err != nil {
			log.Println("Unable to unmarshall", c.Name)
		}
		if err := c.reload(); err != nil {
			log.Println("Unable to reload", c.Name)
		}
	}
}

/*
main method
*/

func main() {
	flag.Parse()
	if *vFlag {
		fmt.Println(VERSION)
		return
	}

	err := downloadBaseCN()
	if err != nil {
		log.Fatal("Something went wrong while downloading base container", err)
	}

	if *aFlag == "create" {
		rand.Seed(time.Now().Unix())
		create()
	} else if *aFlag == "destroy" {
		destroy()
	} else if *aFlag == "reload" {
		reload()
	} else {
		log.Fatal("Unknown action ", *aFlag)
	}
}

/*
template constants
*/

// On host: /containers/name/image/config
const CONFIG_FILE = `
lxc.network.type=veth
lxc.network.link=lxcbr0
lxc.network.flags=up
lxc.network.hwaddr = {{.Hwaddr}}
lxc.utsname = {{.HostName}}
{{if .HasStaticIp}}
	lxc.network.ipv4 = {{.IpConfig}}
{{end}}

lxc.devttydir = lxc
lxc.tty = 4
lxc.pts = 1024
lxc.rootfs = {{.Rootfs}}
lxc.mount  = {{.FstabConfig}}
lxc.arch = amd64
lxc.cap.drop = sys_module mac_admin
lxc.pivotdir = lxc_putold

# uncomment the next line to run the container unconfined:
#lxc.aa_profile = unconfined

lxc.cgroup.devices.deny = a
# Allow any mknod (but not using the node)
lxc.cgroup.devices.allow = c *:* m
lxc.cgroup.devices.allow = b *:* m
# /dev/null and zero
lxc.cgroup.devices.allow = c 1:3 rwm
lxc.cgroup.devices.allow = c 1:5 rwm
# consoles
lxc.cgroup.devices.allow = c 5:1 rwm
lxc.cgroup.devices.allow = c 5:0 rwm
#lxc.cgroup.devices.allow = c 4:0 rwm
#lxc.cgroup.devices.allow = c 4:1 rwm
# /dev/{,u}random
lxc.cgroup.devices.allow = c 1:9 rwm
lxc.cgroup.devices.allow = c 1:8 rwm
lxc.cgroup.devices.allow = c 136:* rwm
lxc.cgroup.devices.allow = c 5:2 rwm
# rtc
lxc.cgroup.devices.allow = c 254:0 rwm
#fuse
lxc.cgroup.devices.allow = c 10:229 rwm
#tun
lxc.cgroup.devices.allow = c 10:200 rwm
#full
lxc.cgroup.devices.allow = c 1:7 rwm
#hpet
lxc.cgroup.devices.allow = c 10:228 rwm
#kvm
lxc.cgroup.devices.allow = c 10:232 rwm
{{range $key, $value := .BindMounts}}
lxc.mount.entry = {{$key}} {{$value}} none bind,rw 0 0
{{end}}
`

//In container: /etc/network/ninterfaces
const INTERFACES_FILE = `
auto lo
iface lo inet loopback
auto eth0
iface eth0 inet {{.Inet}}
`

//In container: /etc/hosts
const HOSTS_FILE = `
127.0.0.1 localhost {{.HostName}}
`

//In container: /etc/hostname
const HOSTNAME_FILE = `
{{.HostName}}
`

//In container: /etc/init/setup-gateway.conf
const SETUP_GATEWAY_FILE = `
description "setup gateway"
start on startup
script
route add -net default gw 10.0.3.1
end script
`

