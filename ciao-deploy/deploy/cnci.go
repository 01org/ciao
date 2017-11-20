// Copyright © 2017 Intel Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package deploy

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/ciao-project/ciao/bat"
	"github.com/ciao-project/ciao/ssntp"
	"github.com/pkg/errors"
)

var cnciImageID = "4e16e743-265a-4bf2-9fd1-57ada0b28904"

func mountImage(ctx context.Context, fp string, mntDir string) (string, error) {
	cmd := SudoCommandContext(ctx, "losetup", "-f", "--show", "-P", fp)
	buf, err := cmd.Output()
	if err != nil {
		return "", errors.Wrapf(err, "Error running: %v", cmd.Args)
	}

	devPath := strings.TrimSpace(string(buf))
	fmt.Printf("Image %s available as %s\n", fp, devPath)

	pPath := fmt.Sprintf("%sp%d", devPath, 2)
	cmd = SudoCommandContext(ctx, "mount", pPath, mntDir)
	err = cmd.Run()
	if err != nil {
		_ = unMountImage(context.Background(), devPath, mntDir)
		return devPath, errors.Wrapf(err, "Error running: %v", cmd.Args)
	}
	fmt.Printf("Device %s mounted as %s\n", pPath, mntDir)

	return devPath, nil
}

func unMountImage(ctx context.Context, devPath string, mntDir string) error {
	var errOut error

	cmd := SudoCommandContext(ctx, "umount", mntDir)
	err := cmd.Run()
	if err != nil {
		if errOut == nil {
			errOut = errors.Wrapf(err, "Error running: %v", cmd.Args)
		}
		fmt.Fprintf(os.Stderr, "Error unmounting: %v\n", err)
	} else {
		fmt.Printf("Directory unmounted: %s\n", mntDir)
	}

	cmd = SudoCommandContext(ctx, "losetup", "-d", devPath)
	err = cmd.Run()
	if err != nil {
		if errOut == nil {
			errOut = errors.Wrapf(err, "Error running: %v", cmd.Args)
		}
		fmt.Fprintf(os.Stderr, "Error removing loopback: %v\n", err)
	} else {
		fmt.Printf("Loopback removed: %s\n", devPath)
	}

	return errOut
}

func getProxy(env string) (string, error) {
	proxy := os.Getenv(strings.ToLower(env))
	if proxy == "" {
		proxy = os.Getenv(strings.ToUpper(env))
	}

	if proxy == "" {
		return "", nil
	}

	if proxy[len(proxy)-1] == '/' {
		proxy = proxy[:len(proxy)-1]
	}

	proxyURL, err := url.Parse(proxy)
	if err != nil {
		return "", fmt.Errorf("Failed to parse %s : %v", proxy, err)
	}
	return proxyURL.String(), nil
}

func copyFiles(ctx context.Context, mntDir string, agentCertPath string, caCertPath string) error {
	p := path.Join(mntDir, "/var/lib/ciao")
	err := SudoMakeDirectory(ctx, p)
	if err != nil {
		return errors.Wrap(err, "Error making certificate directory")
	}

	p = path.Join(mntDir, "/var/lib/ciao/cert-client-localhost.pem")
	err = SudoCopyFile(ctx, p, agentCertPath)
	if err != nil {
		return errors.Wrap(err, "Error copying agent cert to image")
	}

	p = path.Join(mntDir, "/var/lib/ciao/CAcert-server-localhost.pem")
	err = SudoCopyFile(ctx, p, caCertPath)
	if err != nil {
		return errors.Wrap(err, "Error copying CA cert to image")
	}

	p = path.Join(mntDir, "/usr/sbin")
	err = SudoCopyFile(ctx, p, InGoPath("/bin/ciao-cnci-agent"))
	if err != nil {
		return errors.Wrap(err, "Error copying agent binary")
	}

	p = path.Join(mntDir, "/usr/lib/systemd/system")
	err = SudoCopyFile(ctx, p, InGoPath("/src/github.com/ciao-project/ciao/networking/ciao-cnci-agent/scripts/ciao-cnci-agent.service"))
	if err != nil {
		return errors.Wrap(err, "Error copying service file into image")
	}

	p = path.Join(mntDir, "/etc/systemd/system/default.target.wants")
	err = SudoMakeDirectory(ctx, p)
	if err != nil {
		return errors.Wrap(err, "Error making systemd default directory")
	}

	p = path.Join(mntDir, "/etc")
	err = SudoCopyFile(ctx, p, "/etc/resolv.conf")
	if err != nil {
		return errors.Wrap(err, "Error copying temporary resolv.conf")
	}

	httpProxy, err := getProxy("https_proxy")
	if err != nil {
		return errors.Wrap(err, "Error obtaining proxy info")
	}

	proxyEnv := fmt.Sprintf("https_proxy=%s", httpProxy)

	cmd := SudoCommandContext(ctx, proxyEnv, "chroot", mntDir, "swupd", "bundle-add", "dhcp-server")
	err = cmd.Run()
	if err != nil {
		return errors.Wrap(err, "Error adding clear bundle")
	}

	p = path.Join(mntDir, "/etc/resolv.conf")

	err = SudoRemoveFile(ctx, p)
	if err != nil {
		return errors.Wrap(err, "Error removing temporary resolv.conf")
	}

	cmd = SudoCommandContext(ctx, "chroot", mntDir, "systemctl", "enable", "ciao-cnci-agent.service")
	err = cmd.Run()
	if err != nil {
		return errors.Wrap(err, "Error enabling cnci agent on startup")
	}

	p = path.Join(mntDir, "/var/lib/cloud")
	err = SudoRemoveDirectory(ctx, p)
	if err != nil {
		return errors.Wrap(err, "Error removing cloud-init data")
	}

	return nil
}

func prepareImage(ctx context.Context, baseImage string, agentCertPath string, caCertPath string) (_ string, errOut error) {
	preparedImagePath := strings.TrimSuffix(baseImage, ".xz")

	cmd := exec.CommandContext(ctx, "unxz", "-f", "-k", baseImage)
	err := cmd.Run()
	if err != nil {
		return "", errors.Wrap(err, "Error uncompressing cnci image")
	}
	defer func(tempImage string) {
		_ = os.Remove(tempImage)
	}(preparedImagePath)

	rawImagePath := fmt.Sprintf("%s.%s", preparedImagePath, "raw")
	cmd = SudoCommandContext(ctx, "qemu-img", "convert", "-f", "qcow2", "-O", "raw", preparedImagePath, rawImagePath)
	err = cmd.Run()
	if err != nil {
		return "", errors.Wrap(err, "Error converting cnci image")
	}
	defer func() {
		if errOut != nil {
			_ = os.Remove(rawImagePath)
		}
	}()
	preparedImagePath = rawImagePath

	mntDir, err := ioutil.TempDir("", "cnci-mount")
	if err != nil {
		return "", errors.Wrap(err, "Error making mount point directory")
	}
	defer func() {
		fmt.Printf("Removing mount point: %s\n", mntDir)
		err := os.RemoveAll(mntDir)
		if err != nil {
			if errOut == nil {
				errOut = errors.Wrap(err, "Error removing mount point")
			}
		}
	}()

	devPath, err := mountImage(ctx, preparedImagePath, mntDir)
	if err != nil {
		return "", errors.Wrap(err, "Error mounting image")
	}
	defer func() {
		err := unMountImage(context.Background(), devPath, mntDir)
		if err != nil {
			if errOut == nil {
				errOut = errors.Wrap(err, "Error unmounting image")
			}
		}
	}()

	err = copyFiles(ctx, mntDir, agentCertPath, caCertPath)
	if err != nil {
		return "", errors.Wrap(err, "Error copying files into image")
	}

	return preparedImagePath, nil
}

func getCNCIURLs(ctx context.Context) ([]string, error) {
	const knownGoodImage = "19150"
	knownGoodURL := fmt.Sprintf("https://download.clearlinux.org/releases/%[1]s/clear/clear-%[1]s-cloud.img.xz", knownGoodImage)

	req, err := http.NewRequest(http.MethodGet, "https://download.clearlinux.org/latest", nil)
	if err != nil {
		return nil, errors.Wrap(err, "Error downloading clear version info")
	}
	req = req.WithContext(ctx)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("Unable to determine latest clear release (%v), falling back to %s\n", err, knownGoodImage)
		return []string{knownGoodURL}, nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Unexpected status (%s) when determining latest clear release, falling back to %s\n",
			resp.Status, knownGoodImage)
		return []string{knownGoodURL}, nil
	}

	versionBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Unable to read latest clear release (%v), falling back to %s\n", err, knownGoodImage)
		return []string{knownGoodURL}, nil
	}
	version := strings.TrimSpace(string(versionBytes))

	cnciURLs := []string{
		fmt.Sprintf("https://download.clearlinux.org/image/clear-%s-cloud.img.xz", version),
		knownGoodURL,
	}
	return cnciURLs, nil
}

// CreateCNCIImage creates a customised CNCI image in the system
func CreateCNCIImage(ctx context.Context, anchorCertPath string, caCertPath string, imageCacheDir string) (errOut error) {
	agentCertPath, err := GenerateCert(anchorCertPath, ssntp.CNCIAGENT)
	if err != nil {
		return errors.Wrap(err, "Error creating agent certificate")
	}
	defer func() { _ = os.Remove(agentCertPath) }()

	baseURLs, err := getCNCIURLs(ctx)
	if err != nil {
		return err
	}

	var baseImagePath string
	var downloaded bool
	var url int
	for url = 0; url < len(baseURLs); url++ {
		baseImagePath, downloaded, err = DownloadImage(ctx, baseURLs[url], imageCacheDir)
		if err == nil {
			break
		}
		if url+1 < len(baseURLs) {
			fmt.Printf("Error downloading image %s\n", baseURLs[url])
		}
	}

	if err != nil {
		return errors.Wrap(err, "Error downloading image")
	}

	if url > 0 {
		fmt.Printf("Downloaded backup image %s\n", baseURLs[url])
	}

	defer func() {
		if errOut != nil && downloaded {
			_ = os.Remove(baseImagePath)
		}
	}()

	preparedImage, err := prepareImage(ctx, baseImagePath, agentCertPath, caCertPath)
	if err != nil {
		return errors.Wrap(err, "Error preparing image")
	}
	defer func() { _ = os.Remove(preparedImage) }()

	fmt.Printf("Image prepared at: %s\n", preparedImage)

	imageOpts := &bat.ImageOptions{
		ID:         cnciImageID,
		Visibility: "internal",
		Name:       "ciao CNCI image",
	}

	fmt.Printf("Uploading image as %s\n", imageOpts.ID)
	i, err := bat.AddImage(ctx, true, "", preparedImage, imageOpts)
	if err != nil {
		return errors.Wrap(err, "Error uploading image to controller")
	}

	fmt.Printf("CNCI image uploaded as %s\n", i.ID)

	// clean up any old images
	pattern := "clear-*-cloud.img.xz"
	keep := []string{baseImagePath}
	err = CleanupImages(pattern, keep, imageCacheDir)
	if err != nil {
		fmt.Printf("Error cleaning old images: %v", err)
	}

	return nil
}
