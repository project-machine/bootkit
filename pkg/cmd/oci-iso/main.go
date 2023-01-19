package main

import (
	"archive/tar"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/apex/log"
	"golang.org/x/sys/unix"

	ispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/opencontainers/umoci"
	"github.com/opencontainers/umoci/oci/casext"
	"github.com/opencontainers/umoci/oci/layer"
	cli "github.com/urfave/cli"
	"stackerbuild.io/stacker/pkg/lib"
	stackeroci "stackerbuild.io/stacker/pkg/oci"
)

type BootMode int

const (
	PathESPImage    = "loader/images/efi-esp.img"
	PathEltoritoCat = "loader/eltorito.cat"
	PathEltoritoBin = "loader/images/grub-eltorito.bin"
	PathIsohdpfxBin = "loader/images/isohdpfx.bin"
	PathKernelEFI   = "loader/uefi/kernel.efi"
	PathGrubCfg     = "loader/grub/grub.cfg"
	Bios            = "bios"
	BootLayerName   = "oci-live-boot"
	ISOLabel        = "OCI-ISO"
)

const (
	EFIAuto BootMode = iota
	EFIShim
	EFIKernel
)

const SBATContent = `sbat,1,SBAT Version,sbat,1,https://github.com/rhboot/shim/blob/main/SBAT.md
stubby.puzzleos,2,PuzzleOS,stubby,1,https://github.com/puzzleos/stubby
linux.puzzleos,1,PuzzleOS,linux,1,NOURL
`

var EFIBootModeStrings = map[string]BootMode{
	"efi-auto":   EFIAuto,
	"efi-shim":   EFIShim,
	"efi-kernel": EFIKernel,
}

var EFIBootModes = map[BootMode]string{
	EFIAuto:   "efi-auto",
	EFIShim:   "efi-shim",
	EFIKernel: "efi-kernel",
}

type ISOOptions struct {
	EFIBootMode BootMode
	CommandLine string
}

func (opts ISOOptions) Check() error {
	if _, ok := EFIBootModes[opts.EFIBootMode]; !ok {
		return fmt.Errorf("Invalid boot mode %d", opts.EFIBootMode)
	}
	return nil
}

func (opts ISOOptions) MkisofsArgs() ([]string, error) {
	s := []string{}
	/*
		if opts.BiosBoot {
			s = append(s,
				"-eltorito-catalog", PathEltoritoCat,
				"-eltorito-boot", PathEltoritoBin,
				"-no-emul-boot", "-boot-load-size", "4", "-boot-info-table")
		}
	*/

	s = append(s,
		"-eltorito-alt-boot", "-e", PathESPImage, "-no-emul-boot", "-isohybrid-gpt-basdat")

	return s, nil
}

func ociExtractRef(image, dest string) error {

	tmpOciDir, err := ioutil.TempDir("", "extractRef-")
	if err != nil {
		return err
	}
	const tmpName = "xxextract"
	defer os.RemoveAll(tmpOciDir)
	err = lib.ImageCopy(lib.ImageCopyOpts{
		Src:  image,
		Dest: fmt.Sprintf("oci:%s:%s", tmpOciDir, tmpName),
	})
	if err != nil {
		return fmt.Errorf("couldn't extract %s: %w", image, err)
	}
	oci, err := umoci.OpenLayout(tmpOciDir)
	if err != nil {
		return err
	}
	defer oci.Close()

	return unpackLayerRootfs(tmpOciDir, oci, tmpName, dest)
}

func unpackLayerRootfs(ociDir string, oci casext.Engine, tag string, extractTo string) error {
	// UnpackLayer creates rootfs config.json, sha256_<hash>.mtree umoci.json
	// but we want just the contents of rootfs in extractTo
	rootless := syscall.Geteuid() != 0
	log.Infof("extracting %s -> %s (rootless=%v)", tag, extractTo, rootless)

	xdir := path.Join(extractTo, ".extract")
	rootfs := path.Join(xdir, "rootfs")
	defer os.RemoveAll(xdir)

	if err := UnpackLayer(ociDir, oci, tag, xdir, rootless); err != nil {
		return err
	}

	entries, err := ioutil.ReadDir(rootfs)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if err := os.Rename(path.Join(rootfs, entry.Name()), path.Join(extractTo, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func UnpackLayer(ociDir string, oci casext.Engine, tag string, dest string, rootless bool) error {
	manifest, err := stackeroci.LookupManifest(oci, tag)
	if err != nil {
		return fmt.Errorf("couldn't find '%s' in oci: %w", tag, err)
	}

	if manifest.Layers[0].MediaType == ispec.MediaTypeImageLayer ||
		manifest.Layers[0].MediaType == ispec.MediaTypeImageLayerGzip {
		os := layer.UnpackOptions{KeepDirlinks: true}
		if rootless {
			os.MapOptions, err = GetRootlessMapOptions()
			if err != nil {
				return err
			}
		}
		err = umoci.Unpack(oci, tag, dest, os)
		if err != nil {
			return err
		}
	} else {
		if err := unpackSquashLayer(ociDir, oci, tag, dest, rootless); err != nil {
			return err
		}
	}
	return nil
}

// after calling getBootkit, the returned path will have 'bootkit' under it.
func getBootKit(ref string) (func() error, string, error) {
	cleanup := func() error { return nil }
	path := ""
	var err error
	if strings.HasPrefix(ref, "oci:") || strings.HasPrefix(ref, "docker:") {
		var tmpd string
		tmpd, err = ioutil.TempDir("", "getBootKit-")
		if err != nil {
			return cleanup, tmpd, err
		}
		path = tmpd
		if err = ociExtractRef(ref, path); err != nil {
			return cleanup, path, err
		}

		// cleanup = func() error { return os.RemoveAll(tmpd) }
		cleanup = func() error { return nil }
	} else {
		// local dir existing.
		path, err = filepath.Abs(ref)
	}

	if PathExists(filepath.Join(path, "export")) {
		// drop a top level 'export'
		path = filepath.Join(path, "export")
	}

	required := []string{"bootkit"}
	errmsgs := []string{}

	for _, r := range required {
		// for each entry in required, accept an existing dir
		// if no dir, then extract the .tar with that name.
		dirPath := filepath.Join(path, r)
		if isDir(dirPath) {
			continue
		}
	}

	if len(errmsgs) != 0 {
		return cleanup, path, fmt.Errorf("bootkit at %s had errors:\n  %s\n", ref, strings.Join(errmsgs, "\n  "))
	}

	return cleanup, path, nil
}

func untar(tarball, target string) error {
	reader, err := os.Open(tarball)
	if err != nil {
		return err
	}

	if err != nil {
		return err
	}
	defer reader.Close()
	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		path := filepath.Join(target, header.Name)
		info := header.FileInfo()
		if info.IsDir() {
			if err = os.MkdirAll(path, info.Mode()); err != nil {
				return err
			}
			continue
		}

		file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(file, tarReader)
		if err != nil {
			return err
		}
	}
	return nil
}

func isDir(fpath string) bool {
	file, err := os.Open(fpath)
	if err != nil {
		return false
	}
	fi, err := file.Stat()
	if err != nil {
		return false
	}
	return fi.IsDir()
}

type OciBoot struct {
	BootKit    string            `json:"bootkit"`
	BootLayer  string            `json:"boot-layer"`
	Files      map[string]string `json:"files"`
	Layers     []string          `json:"layers"`
	cleanups   []func() error
	bootKitDir string
}

// Create - create an iso in isoFile
func (o *OciBoot) Create(isoFile string, opts ISOOptions) error {
	if err := opts.Check(); err != nil {
		return err
	}
	if err := o.getBootKit(); err != nil {
		return err
	}
	tmpd, err := ioutil.TempDir("", "OciBootCreate-")
	if err != nil {
		return err
	}

	defer os.RemoveAll(tmpd)

	if err := o.Populate(tmpd, opts); err != nil {
		return err
	}

	mkopts, err := opts.MkisofsArgs()
	if err != nil {
		return err
	}
	cmd := []string{
		"xorriso",
		"-compliance", "iso_9660_level=3",
		"-as", "mkisofs",
		"-o", isoFile,
		"-V", ISOLabel,
	}

	cmd = append(cmd, mkopts...)
	cmd = append(cmd, tmpd)

	log.Infof("Executing: %s", strings.Join(cmd, " "))
	if err := RunCommand(cmd...); err != nil {
		return err
	}

	return nil
}

func copyFile(src, dest string) error {
	fin, err := os.Open(src)
	if err != nil {
		return err
	}

	info, err := fin.Stat()
	if err != nil {
		return err
	}

	defer fin.Close()

	fout, err := os.OpenFile(dest, os.O_RDWR|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer fout.Close()

	_, err = io.Copy(fout, fin)

	if err != nil {
		return err
	}

	return nil
}

func ensureDir(dir string) error {
	return os.MkdirAll(dir, 0755)
}

func writeGrubConfig(fname string) error {
	if err := ensureDir(filepath.Dir(fname)); err != nil {
		return err
	}
	const grubCfg = `
if [ "$grub_platform" = "efi" ]; then
    linux="linuxefi"
    initrd="initrdefi"
    serial="serial_efi0"

    insmod efi_gop
    insmod efi_uga
else
    linux="linux"
    initrd="initrd"
    serial="serial"
fi

serial --unit=0 --speed=115200
terminal_output console $serial
terminal_input console $serial

insmod video_bochs
insmod video_cirrus
insmod all_video
insmod gzio
insmod part_gpt
insmod ext2

set default="0"
set gfxpayload=keep
set timeout=7
# Optional longer timeout for dev/test - number is in seconds.
#set timeout=200

menuentry 'Boot OCI:` + BootLayerName + `' --class gnu-linux --class os {
    $linux /krd/kernel verbose console=tty0 console=ttyS0,115200n8 root=soci:name=` + BootLayerName + `,dev=LABEL=` + ISOLabel + `
    $initrd /krd/initrd
}

if [ "$grub_platform" = "efi" -a -e "/loader/uefi/kernel.efi" ]; then
    menuentry 'Boot kernel.efi' --class efi --class os {
        chainloader /loader/uefi/kernel.efi
    }
fi

if [ "$grub_platform" = "efi" -a -e "/loader/uefi/shell.efi" ]; then
    menuentry 'EFI shell' --class efi {
        chainloader /loader/uefi/shell.efi
    }
fi
`
	return ioutil.WriteFile(fname, []byte(grubCfg), fs.FileMode(0644))
}

func (o *OciBoot) genESP(opts ISOOptions, fname string) error {
	// If EFINone was given (nothing set), then use shim if there is one.
	mode := opts.EFIBootMode
	if mode == EFIAuto {
		mode = EFIKernel
		if PathExists(filepath.Join(o.bootKitDir, "bootkit/shim.efi")) {
			mode = EFIShim
		}
	}

	cmdline := ""
	if o.BootLayer != "" {
		// FIXME: cmdline root= should be based on type of o.BootLayer (root=soci or root=oci)
		cmdline = "root=soci:name=" + BootLayerName + ",dev=LABEL=" + ISOLabel
	}
	if opts.CommandLine != "" {
		cmdline = cmdline + " " + opts.CommandLine
	}

	// const DefaultEFI, ShimLoadsEFI = "/EFI/BOOT/BOOTX64.EFI", "/EFI/BOOT/GRUBX64.EFI"
	const EFIBootDir = "/EFI/BOOT/"
	const StartupNSHPath = "STARTUP.NSH"
	KernelEFI := "KERNEL.EFI"
	ShimEFI := "SHIM.EFI"
	const mib = 1024 * 1024
	// should get the total size of all the source files and compute this.
	var size int64 = 0
	var startupNshContent = []string{
		"FS0:",
		"cd FS0:/EFI/BOOT",
	}
	copies := map[string]string{}
	if mode == EFIShim {
		copies[filepath.Join(o.bootKitDir, "bootkit/shim.efi")] = EFIBootDir + ShimEFI
		copies[filepath.Join(o.bootKitDir, "bootkit/kernel.efi")] = EFIBootDir + KernelEFI
		startupNshContent = append(startupNshContent, ShimEFI+" "+KernelEFI+" "+cmdline)
		// Right now kernel.efi is 48M, shim is 1M.  So 64M should be
		// OK, but could get tight quickly.
		size = 100 * mib
	} else if mode == EFIKernel {
		copies[filepath.Join(o.bootKitDir, "bootkit/kernel.efi")] = KernelEFI
		startupNshContent = append(startupNshContent, KernelEFI+" "+cmdline)
		size = 64 * mib
	}

	// need to write a startup.nsh here
	if startupnsh, err := writeTemp([]byte(strings.Join(startupNshContent, "\n"))); err != nil {
		return err
	} else {
		copies[startupnsh] = EFIBootDir + StartupNSHPath
		defer os.Remove(startupnsh)
	}

	if err := ensureDir(filepath.Dir(fname)); err != nil {
		return fmt.Errorf("genESP: Failed to Mkdir(%s)", filepath.Dir(fname))
	}

	if err := genESP(fname, size, copies); err != nil {
		return err
	}

	return nil
}

func genESP(fname string, size int64, copies map[string]string) error {
	fp, err := os.OpenFile(fname, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return fmt.Errorf("Failed to open %s for Create: %w", fname, err)
	}
	// This path is here as I was debugging why the go-diskfs code wasn't creating
	// something that worked with qemu ovmf (uefi didn't find the fs)
	if err := unix.Ftruncate(int(fp.Fd()), size); err != nil {
		log.Fatalf("Truncate '%s' failed: %s", fname, err)
	}
	if err := fp.Close(); err != nil {
		return fmt.Errorf("Failed to close file %s", fname)
	}

	if err := RunCommand("mkfs.fat", "-s", "1", "-F", "32", "-n", "EFIBOOT", fname); err != nil {
		return fmt.Errorf("mkfs.fat failed: %w", err)
	}

	tmpd, err := ioutil.TempDir("", "genESPSystem-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpd)

	for src, dest := range copies {
		log.Debugf("[%s] %s -> %s", fname, src, dest)
		fullDest := filepath.Join(tmpd, dest)

		if err := os.MkdirAll(filepath.Dir(fullDest), 0755); err != nil {
			return err
		}

		fmt.Printf("[%s] %s -> %s\n", fname, src, dest)

		srcFile, err := os.Open(src)
		if err != nil {
			return fmt.Errorf("genESP: Failed to open %s: %w", src, err)
		}
		defer srcFile.Close()

		destFile, err := os.Create(fullDest)
		if err != nil {
			return fmt.Errorf("Failed to open dest '%s': %w", dest, err)
		}
		defer destFile.Close()

		if _, err := io.Copy(destFile, srcFile); err != nil {
			return fmt.Errorf("genESP: Failed to copy from %s -> %s: %w", src, dest, err)
		}
	}

	cmd := []string{"env", "MTOOLS_SKIP_CHECK=1", "mcopy", "-s", "-v", "-i", fname,
		filepath.Join(tmpd, "EFI"), "::EFI"}
	log.Debugf("Running: %s", strings.Join(cmd, " "))
	if err := RunCommand(cmd...); err != nil {
		return err
	}
	return nil
}

func getImageName(ref string) string {
	// should be like 'oci:<path>:<name> - where 'name' might have : in it.
	// this is really simplistic but I just want the 'basename' effect
	// of 'mkdir dest; cp /some/file dest/'
	toks := strings.SplitN(ref, ":", 3)
	return toks[2]
}

// populate the directory with the contents of the iso.
func (o *OciBoot) Populate(target string, opts ISOOptions) error {
	if err := opts.Check(); err != nil {
		return err
	}

	if err := o.genESP(opts, filepath.Join(target, PathESPImage)); err != nil {
		return err
	}

	ociDir := filepath.Join(target, "oci")
	if o.BootLayer != "" {
		log.Infof("Copying BootLayer %s -> %s:%s", o.BootLayer, ociDir, BootLayerName)
		dest := "oci:" + ociDir + ":" + BootLayerName
		err := lib.ImageCopy(lib.ImageCopyOpts{Src: o.BootLayer, Dest: dest, Progress: os.Stderr})
		if err != nil {
			return fmt.Errorf("Failed to copy image from BootLayer '%s'", o.BootLayer)
		}
	}

	if len(o.Layers) != 0 {
		ociDest := "oci:" + ociDir + ":"
		for i, src := range o.Layers {
			dest := ociDest + getImageName(src)
			log.Infof("Copying Layer %d/%d: %s -> %s", i, len(o.Layers), src, dest)
			err := lib.ImageCopy(lib.ImageCopyOpts{Src: src, Dest: dest, Progress: os.Stderr})
			if err != nil {
				return fmt.Errorf("Failed to copy %s: %w", src, err)
			}
		}
	}

	for src, dest := range o.Files {
		if err := copyFile(src, path.Join(target, dest)); err != nil {
			return fmt.Errorf("Failed to copy file '%s' to iso path '%s': %w", src, dest, err)
		}
	}

	return nil
}

func writeTemp(content []byte) (string, error) {
	fh, err := os.CreateTemp("", "writeTemp")
	if err != nil {
		return "", err
	}
	if _, err := fh.Write(content); err != nil {
		os.Remove(fh.Name())
		return "", err
	}
	if err := fh.Close(); err != nil {
		os.Remove(fh.Name())
		return "", err
	}
	return fh.Name(), nil
}

func (o *OciBoot) Cleanup() error {
	for _, c := range o.cleanups {
		if err := c(); err != nil {
			return err
		}
	}
	return nil
}

func (o *OciBoot) getBootKit() error {
	if o.bootKitDir != "" {
		return nil
	}
	cleanup, path, err := getBootKit(o.BootKit)
	o.cleanups = append(o.cleanups, cleanup)
	if err != nil {
		return err
	}
	o.bootKitDir = path
	return nil
}

func doMain(ctx *cli.Context) error {
	if ctx.Bool("debug") {
		log.SetLevel(log.DebugLevel)
	}
	args := ctx.Args()
	if len(args) < 2 {
		return fmt.Errorf("Need at very least 2 args: iso, bootkit-source")
	}
	ociBoot := OciBoot{}

	output := args[0]
	ociBoot.BootKit = args[1]

	if len(args) > 2 {
		ociBoot.BootLayer = args[2]
	}

	if len(args) > 3 {
		ociBoot.Layers = args[3:]
	}

	ociBoot.Files = map[string]string{}
	for _, p := range ctx.StringSlice("insert") {
		toks := strings.SplitN(p, ":", 2)
		if len(toks) != 2 {
			return fmt.Errorf("--insert arg had no 'dest' (src:dest): %s", p)
		}
		ociBoot.Files[toks[0]] = toks[1]
	}

	mode := ctx.String("boot")
	efiMode := EFIAuto
	n, ok := EFIBootModeStrings[mode]
	if !ok {
		return fmt.Errorf("Unexpected --boot=%s. Expect one of: %v", mode, EFIBootModeStrings)
	}
	efiMode = n

	opts := ISOOptions{
		EFIBootMode: efiMode,
		CommandLine: ctx.String("cmdline"),
	}

	defer ociBoot.Cleanup()

	if err := ociBoot.Create(output, opts); err != nil {
		return err
	}

	log.Infof("Wrote iso %s.", output)
	return nil
}

func main() {

	app := cli.NewApp()
	app.Name = "oci-iso"
	app.Usage = "create an iso to boot an oci layer: bootkit boot-layer oci-layers"
	app.Version = "1.0.1"
	app.Action = doMain
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:  "debug",
			Usage: "display additional debug information on stderr",
		},
		cli.StringFlag{
			Name:  "boot",
			Usage: "boot-mode: one of 'efi-shim', 'efi-kernel', or 'efi-auto'",
			Value: EFIBootModes[EFIAuto],
		},
		cli.StringFlag{
			Name:  "cmdline",
			Usage: "cmdline: additional parameters for kernel command line",
		},
		cli.StringSliceFlag{
			Name:  "insert",
			Usage: "list of additional files in <src>:<dest> format to copy to iso",
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatalf("%v\n", err)
	}
}
