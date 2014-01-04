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
)

const VERSION = "0.1"
const CONTAINERS_ROOT_PATH = "/containers"

var version = flag.Bool("v", false, "print version and exit")
var action = flag.String("s", "", "action to perform")
var contId = flag.String("id", "", "id of the container")
var contIp = flag.String("ip", "", "ip of the container")
var contName = flag.String("name", "", "name (and hostname) of the container")
var baseContainerPath = flag.String("b", "/var/lib/lxc/baseCN", "path to the base container rootfs")
var ports = flag.String("p", "", "port to forward host_port:cont_port")
var bindMounts = flag.String("bm", "", "bind mount of type path_host:cont_host,...")

type Container struct {
	BaseContainerPath, Path, RoLayer, WrLayer string
	Rootfs string
	ConfigPath string
	Id, Ip, Hwaddr, Name string
	Port, HostPort int
	BindMounts map[string]string
}

func (c Container) FstabConfig() string {
	return c.RoLayer + "/" + "fstab"
}

func (c Container) IpConfig() string {
	return c.Ip + "/24"
}

func (c Container) setupOnFS() {
	err := os.MkdirAll(c.RoLayer, 0700)
	if err != nil {
		log.Fatal("Unable to create ", c.RoLayer, err)
	}
	err = os.MkdirAll(c.WrLayer, 0700)
	if err != nil {
		log.Fatal("Unable to create ", c.WrLayer, err)
	}
}

func (c Container) forwardPort(add bool) {
	if c.Port == 0 && c.HostPort == 0 {
		return
	}
	t := "-A"
	if !add {
		t = "-D"
	}
	cmd := exec.Command("iptables", "-t", "nat", t, "PREROUTING", "-p", "tcp", "!", "-s", "10.0.3.0/24", "--dport", strconv.Itoa(c.HostPort), "-j", "DNAT", "--to-destination", c.Ip + ":" + strconv.Itoa(c.Port))
	err := cmd.Run()
	if err != nil {
		log.Fatal("Unable to forward port ", err)
	}
}

func (c Container) executeTemplate(content string, path string) {
	tmpl, err := template.New("tmpl").Parse(content)
	if err != nil {
		log.Fatal("Unable to parse template ", err)
	}
	file, err := os.Create(path)
	if err != nil {
		log.Fatal("Unable to create file ", err)
	}
	defer file.Close()
	err = tmpl.Execute(file, c)
	if err != nil {
		log.Fatal("Unable to populate template ", err)
	}
}

func (c Container) setupNetworking() {
	c.executeTemplate(INTERFACES_FILE, c.Rootfs + "/etc/network/interfaces")
	c.executeTemplate(HOSTS_FILE, c.Rootfs + "/etc/hosts")
	c.executeTemplate(HOSTNAME_FILE, c.Rootfs + "/etc/hostname")
	c.executeTemplate(SETUP_GATEWAY_FILE, c.Rootfs + "/etc/init/setup-gateway.conf")
}

func (c Container) destroy() {
	if c.isRunning() {
		log.Fatal("Container is running. Stop it before destroy it")
	}
	c.aufsUnmount()
	err := exec.Command("rm", "-rf", c.Path).Run()
	if err != nil {
		log.Fatal("Unable to rm container", err)
	}
	c.forwardPort(false)
}

func (c Container) aufsMount() {
	mnt := "br=" + c.WrLayer + "=rw:" + c.BaseContainerPath + "=ro"
	err := exec.Command("mount", "-t", "aufs", "-o", mnt, "none", c.RoLayer).Run()
	if err != nil {
		log.Fatal("Unable to AuFS mount", err)
	}
}

func (c Container) aufsUnmount() {
	err := exec.Command("umount", c.RoLayer).Run()
	if err != nil {
		log.Fatal("Unable to umount ro layer ", err)
	}
}

func (c Container) marshall() {
	b, err := json.Marshal(c)
	if err != nil {
		log.Fatal("Unable to marshall container ", err)
	}
	err = ioutil.WriteFile(c.Path + "/.metadata.json", b, 0644)
	if err != nil {
		log.Fatal("Unable to write container metadata ", err)
	}
}

func (c Container) createConfig() {
	c.executeTemplate(CONFIG_FILE, c.RoLayer + "/config")
}

func unmarshall(id string) Container {
	b, err := ioutil.ReadFile(CONTAINERS_ROOT_PATH + "/" + id + "/.metadata.json")
	if err != nil {
		log.Fatal("Unable to read container metadata ", err)
	}
	var c Container
	err = json.Unmarshal(b, &c)
	if err != nil {
		log.Fatal("Unable to unmarshall metadata ", err)
	}
	return c
}

func (c Container) isRunning() bool {
	stdout, err := exec.Command("lxc-info", "-n", c.Name).Output()
	if err != nil {
		log.Fatal("Unable to run lxc-info")
	}
	state := strings.Trim(strings.Split(strings.Split(string(stdout), "\n")[0], ":")[1], " ")
	return state != "STOPPED"
}

func provision() {

}

func create() {
	contPath := CONTAINERS_ROOT_PATH + "/" + *contId
	port := 0
	hostPort := 0
	if len(*ports) > 0 {
		hostPort, _ = strconv.Atoi(strings.Split(*ports, ":")[0])
		port, _ = strconv.Atoi(strings.Split(*ports, ":")[1])
	}
	mounts := make(map[string]string)
	c := Container{
		*baseContainerPath, contPath, contPath + "/image", contPath + "/wlayer",
		contPath + "/image/rootfs",
		contPath + "/image/config",
		*contId, *contIp, "00:11:22:33:44", *contName,
		port, hostPort,
		mounts,
	}

	if len(*bindMounts) > 0 {
		arr := strings.Split(*bindMounts, ",")
		for i := range arr {
			hostMountPath := strings.Split(arr[i], ":")[0]
			contMountPath := c.Rootfs + strings.Split(arr[i], ":")[1]
			if fileExists(contMountPath) == false {
				os.MkdirAll(contMountPath)
			}
			c.BindMounts[strings.Split(arr[i], ":")[0]] = c.Rootfs + strings.Split(arr[i], ":")[1]
		}
	}

	if fileExists(c.Rootfs) {
		log.Fatal("Container with such id already exists")
	}

	c.setupOnFS()
	c.marshall()
	c.aufsMount()
	c.createConfig()
	c.setupNetworking()
	c.forwardPort(true)
	fmt.Println("Container created start using lxc-start -n ", c.Name, " -f ", c.ConfigPath, " -d")
}

func destroy() {
	c := unmarshall(*contId)
	c.destroy()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func main() {
	flag.Parse()
	if *version {
		fmt.Println(VERSION)
		return
	}
	if *action == "provision" {
		provision()
	} else if *action == "create" {
		create()
	} else if *action == "destroy" {
		destroy()
	} else {
		log.Fatal("Unknown action ", action)
	}
}

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

const INTERFACES_FILE = `
auto lo
iface lo inet loopback
auto eth0
iface eth0 inet manual
`

const HOSTS_FILE = `
127.0.0.1 localhost {{.Name}}
`
const HOSTNAME_FILE = `
{{.Name}}
`

const SETUP_GATEWAY_FILE = `
description "setup gateway"
start on startup
script
route add -net default gw 10.0.3.1
end script
`

