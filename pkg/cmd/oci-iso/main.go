package main

import (
	"archive/tar"
	"fmt"
	"io"
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
	BootLayerName   = "live-boot:latest"
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

const layoutTree, layoutFlat, layoutNone = "tree", "flat", ""

type ociPath struct {
	Repo   string
	Name   string
	Tag    string
	layout string
}

func (o ociPath) String() string {
	return "oci:" + o.OciDir() + ":" + o.RefName()
}

func (o *ociPath) OciDir() string {
	if o.layout == layoutTree {
		return filepath.Join(o.Repo, o.Name)
	}
	return o.Repo
}

// the name that you would look for in a manifest
func (o *ociPath) RefName() string {
	if o.layout == layoutTree {
		return o.Tag
	}
	if o.Name == "" {
		return o.Tag
	}
	return o.Name + ":" + o.Tag
}

// the name (namespace) and tag of this entry.
func (o *ociPath) NameAndTag() string {
	if o.Name == "" {
		return o.Tag
	}
	return o.Name + ":" + o.Tag
}

func newOciPath(ref string, layout string) (*ociPath, error) {
	// oci:dir:[name:]tag
	toks := strings.Split(ref, ":")
	num := len(toks)

	p := &ociPath{}
	if num <= 2 {
		return p, fmt.Errorf("Not enough ':' in '%s'. Need 2 or 3, found %d", ref, num-1)
	} else if num > 4 {
		return p, fmt.Errorf("Too many ':' in '%s'. Need 2 or 3, found %d", ref, num-1)
	}

	p.layout = layout
	p.Repo = toks[1]
	switch p.layout {
	case layoutTree, layoutFlat:
	case layoutNone:
		p.layout = layoutTree
		if PathExists(filepath.Join(p.Repo, "index.json")) {
			p.layout = layoutFlat
		}
	default:
		return p, fmt.Errorf("unknown layout %s", layout)
	}

	if num == 3 {
		p.Tag = toks[2]
	} else {
		p.Name = toks[2]
		p.Tag = toks[3]
	}

	return p, nil
}

func ociExtractRef(image, dest string) error {
	tmpOciDir, err := ioutil.TempDir("", "extractRef-")
	if err != nil {
		return err
	}
	const tmpName = "xxextract"
	defer os.RemoveAll(tmpOciDir)

	dp, err := newOciPath("oci:"+tmpOciDir+":"+tmpName, layoutTree)
	if err != nil {
		return err
	}

	if err := doCopy(image, dp.String()); err != nil {
		return fmt.Errorf("copy %s -> %s failed: %w", image, dp.String(), err)
	}

	log.Debugf("ok, that went well, now openLayout(%s)", tmpOciDir)
	ociDir := dp.OciDir()
	oci, err := umoci.OpenLayout(ociDir)
	if err != nil {
		return err
	}
	defer oci.Close()

	log.Debugf("ok, that went well, now unpack %s, %s, %s, %s", tmpOciDir, oci, tmpName, dest)

	return unpackLayerRootfs(ociDir, oci, dp.RefName(), dest)
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

	startupNshContent = append(startupNshContent, "")

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

// copy oci image at src to dest
// for 'oci:' src or dest
// if src or dest is of form:
//    oci:dir:[name:]tag
// Then attempt to support 'zot' layout
// this should also wo
func doCopy(src, dest string) error {
	log.Debugf("copying %s -> %s", src, dest)
	dpSrc, err := newOciPath(src, layoutNone)
	if err != nil {
		return err
	}

	dpDest, err := newOciPath(dest, layoutTree)
	if err != nil {
		return err
	}

	log.Debugf("Copying %s -> %s", dpSrc, dpDest)
	if err := os.MkdirAll(dpDest.OciDir(), 0755); err != nil {
		return fmt.Errorf("Failed to create directory %s for %s", dpDest.OciDir(), dpDest)
	}
	/*
		if strings.HasPrefix(normDest, "oci:") {
			dir := strings.SplitN(normDest, ":", 3)[1]
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("Failed to create dir for %s: %w", dest, err)
			}
		}
	*/
	if err := lib.ImageCopy(lib.ImageCopyOpts{Src: dpSrc.String(), Dest: dpDest.String(), Progress: os.Stderr}); err != nil {
		return fmt.Errorf("Failed copy %s -> %s\n", dpSrc, dpDest)
	}
	return nil
}

// if the
func zotToOCI(ref string) string {
	return ""
}

// given ref like : oci:dir:[name:]tag
// return either zot-layout format (tag in dir/name/index.json):
//    oci:dir[/name]:tag
// or oci layout format (name:tag in dir/index.json)
//    oci:dir:[name:]tag
func adjustOCIRef(ref string) (string, error) {
	// ocit = "oci tree"
	if strings.HasPrefix(ref, "ocit:") {
		toks := strings.Split(ref, ":")
		num := len(toks)
		if num == 4 {
			return "oci:" + toks[1] + "/" + toks[2] + ":" + toks[3], nil
		}
		return "", fmt.Errorf("ocit ref %s has %d toks", ref, num)
	}
	if !strings.HasPrefix(ref, "oci:") {
		return ref, nil
	}

	var dirPath, name, tag string
	toks := strings.Split(ref, ":")
	num := len(toks)

	if num <= 2 {
		return "", fmt.Errorf("Not enough ':' in '%s'. Need 2 or 3, found %d", ref, num-1)
	} else if num == 3 {
		// "oci":dir:name -> turn this into oci:dir:name:latest
		// which is what you'd get if you uploaded such a thing to zot
		dirPath = toks[1]
		name = toks[2]
	} else if num == 4 {
		dirPath = toks[1]
		name = toks[2]
		tag = toks[3]
	} else {
		return "", fmt.Errorf("Too many ':' in '%s'. Need 2 or 3, found %d", ref, num-1)
	}

	dirHasIndex := PathExists(filepath.Join(dirPath, "index.json"))
	log.Debugf("dirPath %s : %t", dirPath, dirHasIndex)

	if dirHasIndex {
		// existing oci repo
		if num == 3 {
			return "oci:" + dirPath + ":" + name, nil
		}
		// single oci dir layout.
		log.Debugf("ocilayout: %s -> %s", ref, "oci:"+dirPath+":"+name+":"+tag)
		return "oci:" + dirPath + ":" + name + ":" + tag, nil
	}

	// zot layout
	log.Debugf("zotlayout: %s -> %s", ref, "oci:"+dirPath+"/"+name+":"+tag)
	return "oci:" + dirPath + "/" + name + ":" + tag, nil
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
		if err := doCopy(o.BootLayer, dest); err != nil {
			return fmt.Errorf("Failed to copy image from BootLayer '%s': %w", o.BootLayer, err)
		}
	}

	log.Infof("ok, copied the bootlayer name")

	if len(o.Layers) != 0 {
		ociDest := "oci:" + ociDir + ":"
		for i, src := range o.Layers {
			dSrc, err := newOciPath(src, layoutNone)
			if err != nil {
				return err
			}
			dest := ociDest + dSrc.NameAndTag()
			log.Infof("Copying Layer %d/%d: %s -> %s", i+1, len(o.Layers), src, dest)
			if err := doCopy(src, dest); err != nil {
				return fmt.Errorf("Failed to copy %s -> %s: %w", src, dest, err)
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
