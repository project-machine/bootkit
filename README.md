# bootkit

## bootkit layer
The build of bootkit creates a layer 'bootkit' that has ovmf (kvm firmware), shim, kernel, and initramfs artifacts in an easily available and organized format.

The list of files in a bootkit layer are:

 * bootkit/mos/initrd-mos.cpio.gz - The files needed for 'mos' functionality in an initramfs. This is effectively a "mos initrd module" that can be appended to initrd/core.cpio.gz to give mos functionality.

 * bootkit/stubby/stubby.efi - A build of [stubby](https://github.com/puzzleos/stubby). The consumer combines stubby, kernel, initramfs and cmdline to create a UKI.

 * bootkit/ovmf/ovmf-code.fd, bootkit/ovmf/ovmf-vars.fd - A build of OVMF code and vars.  The vars are empty, they contain no built-in PK, KEK or DB values, and are expected to be customized before use.  Vars can be customized via [virt-firmware](https://pypi.org/project/virt-firmware/).

 * bootkit/initrd/firmware.cpio.gz - This contains early microcode for linux kernel.  If used, it should be the first content in an initramfs and should not be compressed.  See linux kernel [doc](https://github.com/torvalds/linux/blob/master/Documentation/arch/x86/microcode.rst) for more information.

 * bootkit/kernel/version - text file containing 'uname -r' for the kernel.

 * bootkit/kernel/vmlinuz - linux kernel for booting.

 * bootkit/kernel/modules.squashfs - A squashfs archive of kernel modules.  The top level directory in it is the output of `uname -r`. It can be directly consumed by mounting over /lib/modules.

 * bootkit/kernel/initrd-modules.cpio.gz - a cpio archive of kernel modules that are typically used in an initramfs. (modules.squashfs is a strict superset)

 * bootkit/shim/shim.efi - a build of [shim](https://github.com/rhboot/shim) with no included VENDOR_DB.  This needs to be customized by adding a signature list. See ([cert-to-efi-sig-list(1)](https://manpages.ubuntu.com/manpages/jammy/man1/cert-to-efi-sig-list.1.html) for information on signature list format.

## customized layer
Customized layer can be built with `make custom KEYSET_D=/path/to/keyset`.

A keyset is needed to build the customized layer.  the path to a keyset directory in the same format as [keys](https://github.com/project-machine/keys/) repository must be provided in the KEYSET_D variable.

Certificates from keyset are embedded into the produced artifacts. Private keys from the provided keyset are used to sign the artifacts.

The customized layer build output contains:

 * customized/ovmf-code.fd, customized/ovmf-vars.fd - OVMF code and vars.  Vars are populated with uefi-db, uefi-pk, uefi-kek.
 * customized/shim.efi - A shim that contains in it's DB the certificates for `uki-production`, `uki-tpm`, and `uki-limited`.  It is signed with the `uefi-pk` private key.
 * customized/kernel.efi - a Universal Kernel Image.  It contains the `manifest-ca/cert.pem` key at /manifestCA.pem and is signed with `uki-production` private key.


## oci-boot
oci-boot is a tool that can be used to create a bootable iso or disk image from the
files in a bootkit.

After building bootkit and building oci-boot, you can do:

    $ skopeo copy docker://.../rootfs:name-squashfs oci:/tmp/oci.d:rootfs-squashfs
    FIXME: need to soci sign the rootfs

    $ ./pkg/oci-boot out.img \
        oci:$PWD/../build-bootkit/oci:bootkit-squashfs \
        oci:/tmp/oci.d:rootfs-squashfs


## Build
Things that can be defined during this build:
 * KEYSET_D - Make sets this to the user's machine/trust/keys/snakeoil
   directory.  The keyset (snakeoil) can be changed by setting KEYSET
   variable to make (`make KEYSET=myset`).
 * DOCKER_BASE - this should reference a docker url that has 'ubuntu:jammy' in it
   ie, setting to 'docker://' (the default) would use the official docker repos.
 * UBUNTU_MIRROR - this is a url to a ubuntu package mirror.
   default value is http://archive.ubuntu.com/ubuntu
