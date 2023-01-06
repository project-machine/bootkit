# bootkit
Boot files

Bootkit publishes a oci image that contains 4 tar files:

 * /export/version - a text file containing the kernel version.
 * /export/boot.tar - contains the files in traditional linux boot/ directory but under `boot/<version>`:

    * System.map
    * vmlinuz
    * config
    * version

 * /export/initrd.tar - contains artifacts for building an initramfs under a top level `initrd/` directory:

    * initrd/firmware.cpio.gz - the firmware bits of an initramfs update (kernel/x86/microcode/)
    * initrd/core.cpio.gz - the core functionality of an initramfs
    * initrd/modules.cpio.gz - the modules kernel modules to be added to an initramfs (`lib/modules/<version>`)

 * /export/modules.tar - contains complete `lib/modules/<version>` for this kernel.
