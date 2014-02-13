### Creating a base container (ubuntu only for now)

* Use `lxc-create` to create the base container rootfs
* Run `apt-get clean` inside the container to clear packages cache
* Rm `rootfs/dev` and create an empty one i.e:

````bash
mknod -m 666 dev/null c 1 3
mknod -m 666 dev/zero c 1 5
mknod -m 666 dev/random c 1 8
mknod -m 666 dev/urandom c 1 9
mkdir -m 755 dev/pts
mkdir -m 1777 dev/shm
mknod -m 666 dev/tty c 5 0
mknod -m 600 dev/console c 5 1
mknod -m 666 dev/tty0 c 4 0
mknod -m 666 dev/full c 1 7
mknod -m 600 dev/initctl p
mknod -m 666 dev/ptmx c 5 2
````
* Tar the container: `tar --numeric-owner -cpjf baseCN.tar.gz baseCN`
* Calculate md5 hash of the tar: `md5sum baseCN.tar.gz`
* Upload both the hash and the tar on thin-lxc bucket and make it public