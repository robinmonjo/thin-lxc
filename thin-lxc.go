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
)

const VERSION = "0.1"
const CONTAINERS_ROOT_PATH = "/containers"

/*
Flag parsing
*/

var vFlag = flag.Bool("v", false, "print version and exit")

var aFlag = flag.String("s", "", "action to perform")
var idFlag = flag.String("id", "", "id of the container")
var ipFlag = flag.String("ip", "", "ip of the container")
var nFlag = flag.String("name", "", "name (and hostname) of the container")
var bFlag = flag.String("b", "/var/lib/lxc/baseCN", "path to the base container rootfs")
var pFlag = flag.String("p", "", "port to forward host_port:cont_port")
var mFlag = flag.String("bm", "", "bind mount of type path_host:cont_host,...")

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

func (c Container) FstabConfig() string {
	return c.RoLayer + "/" + "fstab"
}

func (c Container) IpConfig() string {
	return c.Ip + "/24"
}

func (c Container) setupOnFS() {
	if err := os.MkdirAll(c.RoLayer, 0700); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(c.WrLayer, 0700); err != nil {
		log.Fatal(err)
	}
}

func (c Container) iptablesRuleDo(action string) error {
	cmd := exec.Command("iptables", "-t", "nat", action,
	 										"PREROUTING", "-p", "tcp", "!", "-s", "10.0.3.0/24",
	 										"--dport", strconv.Itoa(c.HostPort), "-j", "DNAT",
	 										"--to-destination", c.Ip + ":" + strconv.Itoa(c.Port))
	return cmd.Run()
}

func (c Container) forwardPort() {
	if c.Port == 0 && c.HostPort == 0 {
		return
	}
	if err := c.iptablesRuleDo("-C"); err == nil {
		log.Fatal("Trying to add iptables rule that already exists")
	}
	c.iptablesRuleDo("-A")
}

func (c Container) unforwardPort() {
	if err := c.iptablesRuleDo("-C"); err != nil { //rule doesn't exists
		return
	}
	c.iptablesRuleDo("-D")
}

func (c Container) executeTemplate(content string, path string) {
	tmpl, err := template.New("thin-lxc").Parse(content)
	if err != nil {
		log.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	if err := tmpl.Execute(file, c); err != nil {
		log.Fatal(err)
	}
}

func (c Container) configureFiles() {
	c.executeTemplate(CONFIG_FILE, c.RoLayer + "/config")
	c.executeTemplate(INTERFACES_FILE, c.Rootfs + "/etc/network/interfaces")
	c.executeTemplate(HOSTS_FILE, c.Rootfs + "/etc/hosts")
	c.executeTemplate(HOSTNAME_FILE, c.Rootfs + "/etc/hostname")
	c.executeTemplate(SETUP_GATEWAY_FILE, c.Rootfs + "/etc/init/setup-gateway.conf")
}

func (c Container) destroy() {
	if c.isRunning() {
		log.Fatal("Container is running. Stop it before destroying it")
	}
	c.aufsUnmount()
	if err := exec.Command("rm", "-rf", c.Path).Run(); err != nil {
		log.Fatal(err)
	}
	c.unforwardPort()
}

func (c Container) aufsMount() {
	mnt := "br=" + c.WrLayer + "=rw:" + c.BaseContainerPath + "=ro"
	if err := exec.Command("mount", "-t", "aufs", "-o", mnt, "none", c.RoLayer).Run(); err != nil {
		log.Fatal(err)
	}
}

func (c Container) aufsUnmount() {
	if err := exec.Command("umount", c.RoLayer).Run(); err != nil {
		log.Fatal(err)
	}
}

func (c Container) marshall() {
	b, err := json.Marshal(c)
	if err != nil {
		log.Fatal(err)
	}
	if err := ioutil.WriteFile(c.Path + "/.metadata.json", b, 0644); err != nil {
		log.Fatal(err)
	}
}

func (c Container) isRunning() bool {
	stdout, err := exec.Command("lxc-info", "-n", c.Name).Output()
	if err != nil {
		log.Fatal(err)
	}
	state := strings.Trim(strings.Split(strings.Split(string(stdout), "\n")[0], ":")[1], " ")
	return state != "STOPPED"
}

/*
Helper methods
*/
func unmarshall(id string) Container {
	b, err := ioutil.ReadFile(CONTAINERS_ROOT_PATH + "/" + id + "/.metadata.json")
	if err != nil {
		log.Fatal(err)
	}
	var c Container
	if err = json.Unmarshal(b, &c); err != nil {
		log.Fatal(err)
	}
	return c
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func randomHwaddr() string {
	rand.Seed(time.Now().Unix())
	arr := []string{"00:16:3e"}
	for i := 0; i < 3; i++ {
		arr = append(arr, fmt.Sprintf("%0.2x", rand.Intn(256)))
	}
	return strings.Join(arr, ":")
}

/*
Action methods
*/

func provision() {

}

func create() {
	path := CONTAINERS_ROOT_PATH + "/" + *idFlag
	port := 0
	hostPort := 0
	if len(*pFlag) > 0 {
		hostPort, _ = strconv.Atoi(strings.Split(*pFlag, ":")[0])
		port, _ = strconv.Atoi(strings.Split(*pFlag, ":")[1])
	}

	c := Container{
		*bFlag,                      //BaseContainerPath
		path,                        //Path
    
		path + "/image",             //RoLayer 
		path + "/wlayer",            //RwLayer
		path + "/image/rootfs",      //Rootfs
		path + "/image/config",      //ConfigPath

		*idFlag,                     //Id
		*ipFlag,                     //Ip
		randomHwaddr(),              //Hwaddr
		*nFlag,                      //Name
		port,                        //Port
		hostPort,                    //HostPort
		make(map[string]string),     //BindMounts
	}

	if fileExists(c.Rootfs) {
		log.Fatal("Container with such id already exists")
	}

	if len(*mFlag) > 0 {
		arr := strings.Split(*mFlag, ",")
		for i := range arr {
			hostMountPath := strings.Split(arr[i], ":")[0]
			contMountPath := c.Rootfs + strings.Split(arr[i], ":")[1]
			if fileExists(contMountPath) == false {
				os.MkdirAll(contMountPath, 0700)
			}
			c.BindMounts[hostMountPath] = contMountPath
		}
	}

	c.setupOnFS()
	c.marshall()
	c.aufsMount()
	c.configureFiles()
	c.forwardPort()
	fmt.Println("Container created start using: \"lxc-start -n", c.Name,"-f", c.ConfigPath, "-d\"")
}

func destroy() {
	c := unmarshall(*idFlag)
	c.destroy()
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
	if *aFlag == "provision" {
		provision()
	} else if *aFlag == "create" {
		create()
	} else if *aFlag == "destroy" {
		destroy()
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

