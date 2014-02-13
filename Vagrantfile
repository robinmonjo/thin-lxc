Vagrant::Config.run do |config|
  config.vm.box = "lxc-ready-precise64"
  config.vm.box_url = "http://nitron-vagrant.s3-website-us-east-1.amazonaws.com/vagrant_ubuntu_12.04.3_amd64_virtualbox.box"

  config.vm.provision :shell, :inline => <<EOF

sudo apt-get update -qq
sudo DEBIAN_FRONTEND=noninteractive apt-get install -q -y golang lxc curl

echo "--------------------------------------"
echo "Ubuntu 12.04 installed with 3.8 kernel (AuFS and full lxc support)"
echo "--------------------------------------"

EOF
end