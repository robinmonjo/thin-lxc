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
)

const VERSION = "0.2"
const CONTAINERS_ROOT_PATH = "/containers"

/*
Flag parsing
*/

var vFlag = flag.Bool("v", false, "print version and exit")

var aFlag = flag.String("a", "", "action to perform")
var idFlag = flag.String("id", "", "id of the container")
var ipFlag = flag.String("ip", "", "ip of the container")
var nFlag = flag.String("n", "", "name (and hostname) of the container")
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

	Id string
	Ip string
	Hwaddr string
	Name string

	Port int
	HostPort int

	BindMounts map[string]string
}

func (c *Container) FstabConfig() string {
	return c.RoLayer + "/" + "fstab"
}

func (c *Container) IpConfig() string {
	return c.Ip + "/24"
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
	if err := c.executeTemplate(CONFIG_FILE, c.RoLayer + "/config"); err != nil {
		return err
	}
	if err := c.executeTemplate(INTERFACES_FILE, c.Rootfs + "/etc/network/interfaces"); err != nil {
		return err
	}
	if err := c.executeTemplate(HOSTS_FILE, c.Rootfs + "/etc/hosts"); err != nil {
		return err
	}
	if err := c.executeTemplate(HOSTNAME_FILE, c.Rootfs + "/etc/hostname"); err != nil {
		return err
	}
	if err := c.executeTemplate(SETUP_GATEWAY_FILE, c.Rootfs + "/etc/init/setup-gateway.conf"); err != nil {
		return err
	}
	return nil
}

func (c *Container) aufsMount() error {
	mnt := "br=" + c.WrLayer + "=rw:" + c.BaseContainerPath + "=ro"
	return runCmdWithDetailedError(exec.Command("mount", "-t", "aufs", "-o", mnt, "none", c.RoLayer))
}

func (c *Container) aufsUnmount(tryCount int) error {
	if err := runCmdWithDetailedError(exec.Command("umount", c.RoLayer)); err != nil {
		if tryCount >= 0 {
			time.Sleep(1 * time.Second)
			c.aufsUnmount(tryCount - 1)
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
		c.BindMounts[hostMntPath] = c.Rootfs + contMntPath
		if fileExists(c.BindMounts[hostMntPath]) {
			continue
		}

		var err error
		if path.Ext(c.BindMounts[hostMntPath]) == "" {
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

func (c *Container) isRunning() bool {
	stdout, err := exec.Command("lxc-info", "-n", c.Name).Output()
	if err != nil {
		log.Println("lxc-info failed", err)
		return false
	}
	state := strings.Trim(strings.Split(strings.Split(string(stdout), "\n")[0], ":")[1], " ")
	return state != "STOPPED"
}

func (c *Container) create() error {
	if err := c.setupOnFS(); err != nil {
		return err
	}
	if err := c.marshall(); err != nil {
		return err
	}
	if err := c.aufsMount(); err != nil {
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
	if err := c.aufsUnmount(5); err != nil {
		return err
	}
	return c.cleanupFS()
}

func (c *Container) reload() error {
	if c.isMounted() == false {
		if err := c.aufsMount(); err != nil {
			return err
		}
	}
	return c.forwardPort()
}

/*
Helper methods
*/
func unmarshall(id string) (*Container, error) {
	b, err := ioutil.ReadFile(CONTAINERS_ROOT_PATH + "/" + id + "/.metadata.json")
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
Action methods
*/

func create() {
	path := CONTAINERS_ROOT_PATH + "/" + *idFlag
	hostPort, port := parsePortsArg(*pFlag)

	c := &Container{
		*bFlag,                               //BaseContainerPath
		path,                                 //Path
    
		path + "/" + *nFlag,                  //RoLayer 
		path + "/.wlayer",                    //RwLayer
		path + "/" + *nFlag + "/rootfs",      //Rootfs
		path + "/" + *nFlag + "/config",      //ConfigPath

		*idFlag,                              //Id
		*ipFlag,                              //Ip
		randomHwaddr(),                       //Hwaddr
		*nFlag,                               //Name
		port,                                 //Port
		hostPort,                             //HostPort
		parseBindMountsArg(*mFlag),           //BindMounts
	}

	if fileExists(c.Rootfs) {
		log.Fatal("Container with such id already exists")
	}
	
	if err := c.create(); err != nil {
		log.Fatal("Unable to create container", err)
	}

	fmt.Println("Container created start using: \"lxc-start -n", c.Name,"-f", c.ConfigPath, "-d\"")
}

func destroy() {
	c, err := unmarshall(*idFlag)
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
	//after a reboot, AuFS mount and iptables rules will be deleted, reload will reset everything
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
			log.Fatal("Unable to unmarshall", c.Name)
		}
		if err := c.reload(); err != nil {
			log.Fatal("Unable to reload", c.Name)
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

// On host: /containers/id/image/config
const CONFIG_FILE = `
lxc.network.type=veth
lxc.network.link=lxcbr0
lxc.network.flags=up
lxc.network.hwaddr = {{.Hwaddr}}
lxc.utsname = {{.Name}}
lxc.network.ipv4 = {{.IpConfig}}

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
iface eth0 inet manual
`

//In container: /etc/hosts
const HOSTS_FILE = `
127.0.0.1 localhost {{.Name}}
`

//In container: /etc/hostname
const HOSTNAME_FILE = `
{{.Name}}
`

//In container: /etc/init/setup-gateway.conf
const SETUP_GATEWAY_FILE = `
description "setup gateway"
start on startup
script
route add -net default gw 10.0.3.1
end script
`

