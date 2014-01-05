## thin-lxc

`thin-lxc` is a command line tool that extends [LXC](http://linuxcontainers.org/). Goals are:

* allow instant and lightweight creation of container using [AuFS](http://en.wikipedia.org/wiki/Aufs)
* automatically configure packet forwarding between host and containers (using iptables)
* automatically configure bind mount of host file in containers
* assign static ip to containers
* easily provision a container with a local or remote script

`thin-lxc` is not meant to replace LXC. It's an extension and is then used in parallel with LXC:

````bash
#create an empty container that will be used as a base for others
lxc-create -t ubuntu -n baseCN

#create a container from the base container instantely
thin-lxc -s create -id CONTAINER_ID -name myContainer -ip 10.0.3.67 -p 3000:3010 -bm /app/myApp:/app,app/myApp/log:/log

#start the new container
lxc-start -n myContainer -f /containers/CONTAINER_ID/image/config -d

#use every lxc command you like (lxc-stop, lxc-shutdown, lxc-freeze ...)

lxc-shutdown -n myContainer

thin-lxc -s destroy -id CONTAINER_ID
````

### Create a container

`thin-lxs -s create -b /var/lib/lxc/cont -id <id> -name <name> -ip <10.0.3.xxx> [-p <host_port>:<cont_port>] [-bm <host_path>:<cont_path>,<host_path>:<cont_path>,...]`

Options:
* `-b`: the container to use as basis (created using lxc-create)
* `-id`: a unique id
* `-name`: name of the container to use with LXC `-n` option and container hostname
* `-ip`: a static ip that must be in 10.0.3.0/24
* `-p`: port to forward e.g: `3000:3010` will forward packets coming on host:3000 to container:3010
* `-bm`: bind mount points e.g: `/home/ubuntu/app:/app,/home/ubuntu/app/log:/var/log` will mount host's files/folders `/home/ubuntu/app` and `/home/ubuntu/app/log` respectively to `/app` and `/var/log` inside the container.

This will create a container in `/containers`. File system will be like :

````
/containers
	container_id/
		image/     #read only clone of the container used as basis
			config   
			fstab
			rootfs/  
		wlayer/    #all write on image are forwarded here (AuFS magic)
````

### Destroy a container

`thin-lxc -s destroy -id <id>`

Options:
* `-id`: id of the container to destroy.

This will basically just clean up the filesystem (`/containers/container_id`). It is user responsibility to stop the container before (`lxc-shutdown / lxc-stop`)

### Provision a container

Coming soon ...

### Limitations

* host must be a ubuntu box and AuFS compatible
* use the default network bridge provided by LXC on ubuntu
* ip address given to containers must be in 10.0.3.0/24
* container must use upstart (not system.d)
* containers will be created in `/containers` (will add an option later to choose where to set the root path for all containers)

### TODO

* finish test suite
* implement provision
* test behaviour after reboot
* see if it's possible to redirect localhost:port to container:port in some way
* allow use of DHCP for container ip assignment
* check system compatibility (AuFS, iptables ...)
* test using a container created with `thin-lxc` as base container for another one