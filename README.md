# bootkit

## bootkit layer
The build of bootkit uses a reference to a 'keys' tarball like seen
at https://github.com/project-machine/keys/tree/snakeoil/

This can be defined by substituting KEYS_REPO during the stacker build.

Bootkit publishes a oci image that contains these files:
 * boot.tar - tarball of normal linux distribution boot/ files.
 * shim.efi - a shim loader signed with the uefi-db keys.
 * kernel.efi - a universal kernel initrd signed with the production (uki-production) keys.
 * ovmf-vars.fd, ovmf-code.fd - OVMF files for qemu that are populated
   with the uki-limited, uki-production, and uki-tpm keys.

## oci-boot
oci-boot is a tool that can be used to create a bootable iso or disk image from the
files in a bootkit.

After building bootkit and building oci-boot, you can do:

    $ skopeo copy docker://.../rootfs:name-squashfs oci:/tmp/oci.d:rootfs-squashfs
    FIXME: need to soci sign the rootfs

    $ ./pkg/oci-iso out.iso \
        oci:$PWD/../build-bootkit/oci:bootkit-squashfs \
        oci:/tmp/oci.d:rootfs-squashfs


## Build
Things that can be defined during this build:
 * KEYS_REPO - default value is https://github.com/project-machine/keys/tree/snakeoil/
 * DOCKER_BASE - this should reference a docker url that has 'ubuntu:jammy' in it
   ie, setting to 'docker://' (the default) would use the official docker repos.
 * UBUNTU_MIRROR - this is a url to a ubuntu package mirror.
   default value is http://archive.ubuntu.com/ubuntu
