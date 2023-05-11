package shim

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"testing"

	efi "github.com/canonical/go-efilib"
	"github.com/project-machine/bootkit/cert"
	"github.com/project-machine/bootkit/obj"
)

func TestShimHead(t *testing.T) {
	var dbSize, dbxSize uint32 = 925, 0
	headerSize := uint32(16)
	header, err := vendorDBSectionHeader(int(dbSize), int(dbxSize))
	if err != nil {
		t.Errorf("VendorDBSectionHeader failed: %v", err)
	}

	ctable := shimCertTable{}
	if err := binary.Read(bytes.NewReader(header), nativeEndian, &ctable); err != nil {
		t.Errorf("binary.Read into ctable failed: %v", err)
	}

	if ctable.AuthOffset != headerSize {
		t.Errorf("ctable.AuthSize found %d, expected %d", ctable.AuthOffset, headerSize)
	}

	if ctable.DeAuthOffset != (headerSize + dbSize) {
		t.Errorf("ctable.DeAuthOffset found %d, expected %d", ctable.DeAuthOffset, headerSize+dbxSize)
	}

	if ctable.AuthSize != dbSize {
		t.Errorf("ctable.AuthSize found %d, expected %d", ctable.AuthSize, dbSize)
	}

	if ctable.DeAuthSize != dbxSize {
		t.Errorf("ctable.AuthSize found %d, expected %d", ctable.DeAuthSize, dbxSize)
	}
}

func copyFileContents(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return
	}
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		return
	}
	err = out.Sync()
	return
}

func TestShimUpdate(t *testing.T) {
	sigdataList, err := cert.LoadSignatureDataDirs(
		"/ssd/smoser/machine/keys/uki-limited",
		"/ssd/smoser/machine/keys/uki-production",
		"/ssd/smoser/machine/keys/uki-tpm",
	)
	if err != nil {
		t.Fatalf("Failed LoadSignatureDataDirs: %v", err)
	}

	sigDB := cert.NewEFISignatureDatabase(sigdataList)
	sigxDB := cert.NewEFISignatureDatabase([]*efi.SignatureData{})

	vendorSectionFile := "/tmp/vendor.section"
	fp, err := os.Create(vendorSectionFile)
	if err != nil {
		t.Fatalf("Failed to open /tmp/sm.esl: %v", err)
	}

	if err := VendorDBSectionWrite(fp, sigDB, sigxDB); err != nil {
		t.Fatalf("Failed vdbwrite: %v", err)
	}

	fp.Close()

	shimEfiIn := "/ssd/smoser/machine/build-bootkit/stacker/imports/customized/bootkit/shim/shim.efi"
	shimEfi := "/tmp/shim.efi"
	if err := copyFileContents(shimEfiIn, shimEfi); err != nil {
		t.Fatalf("Failec copy shim")
	}
	// /ssd/smoser/machine/build-bootkit/stacker/imports/customized/bootkit/shim/shim.efi
	// func SetSections(objpath string, sections ...SectionInput) error {
	//     set -- objcopy "--remove-section=.vendor_cert" \
	//        "--add-section=.vendor_cert=$dbsection" \
	//		        "--change-section-vma=.vendor_cert=0xb4000" \
	//				        "$input" "$output"
	sections := []obj.SectionInput{
		{Name: ".vendor_cert", VMA: 0xb4000, Path: vendorSectionFile}}

	if err := obj.SetSections(shimEfi, sections...); err != nil {
		t.Fatalf("failed set sections: %s", err)
	}

}
