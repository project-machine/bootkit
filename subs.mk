DOCKER_BASE ?= docker://
UBUNTU_MIRROR ?= http://archive.ubuntu.com/ubuntu

STACKER_SUBS = \
	--substitute=DOCKER_BASE=$(DOCKER_BASE) \
	--substitute=UBUNTU_MIRROR=$(UBUNTU_MIRROR) \
	--substitute=TOP_D=$(TOP_D)
