Vagrant::Config.run do |config|
  config.vm.box = "precise64"
  config.vm.box_url = "http://files.vagrantup.com/precise64.box"

  config.vm.provision :shell, :inline => <<EOF

sudo apt-get update -qq
sudo DEBIAN_FRONTEND=noninteractive apt-get install -q -y golang lxc
sudo lxc-create -t ubuntu -n baseCN #create a base container

EOF
end