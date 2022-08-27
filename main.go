package main

import (
	"containerCrashMonitor/pkg/utils"
	"context"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	docker "github.com/docker/docker/client"
	"os/signal"
	//      openContainer "github.com/opencontainers/image-spec/specs-go/v1"
	"log"
	"os"
	"path"
	"regexp"
	"strings"
	"time"
)

var BaseImageName = "busybox:1.35.0"

func CrashedTooMuch(imageMatch string, interval, maxCount int) (string, bool) {
	id := ""
	client, err := docker.NewClientWithOpts(docker.FromEnv)
	if err != nil {
		fmt.Errorf("error creating docker client: %v", err)
		return id, false
	}
	ts := time.Now().Add(-time.Second * time.Duration(interval)).Unix()

	ctx, _ := context.WithTimeout(context.Background(), time.Millisecond*200)
	events, errs := client.Events(ctx, types.EventsOptions{
		Since: fmt.Sprintf("%d", ts),
	})
	dieCount := 0

	regex, _ := regexp.Compile(imageMatch)

	for {
		select {
		case event, open := <-events:
			if !open {
				return id, false
			}
			imageName := event.Actor.Attributes["image"]
			if imageName == "" {
				continue
			}
			if regex.MatchString(imageName) {
				if event.Action == "die" {
					log.Println("die event for", imageName)
					dieCount++
					id = event.Actor.ID
				}
			}
			if dieCount >= maxCount {
				return id, true
			}

		case _, open := <-errs:
			if !open {
				return "", false
			}
			//log.Println(e)

			return "", false
		}
	}
}

func main() {

	if len(os.Args) < 3 {
		fmt.Println("Usage: ./containerCrashMonitor <image name> [volume name ...]")
		return
	}
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	imageName := os.Args[1]
	volumes := os.Args[2:]
	fmt.Println("image name: ", imageName)
	fmt.Println("volumes: ", volumes)
	fmt.Println("running...")
	if !CheckImageExists(BaseImageName) {
		PullBusyboxImage()
	}
	// tick := time.Tick(time.Second * 180)

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	ticker := time.NewTicker(time.Second * 180)
	defer ticker.Stop()

loop:
	for {
		_, failedTooMuch := CrashedTooMuch(imageName, 180, 3)
		if failedTooMuch {
			log.Println("crashed too much")
			err := EmptyVolume(volumes)
			if err != nil {
				log.Println(err)
			}
			time.Sleep(time.Second * 180)
			//return
		}

		select {
		case <-ticker.C:
			continue
		case <-interrupt:
			break loop
		}
	}

}

func CheckImageExists(imageName string) bool {
	client, _ := docker.NewClientWithOpts(docker.FromEnv)
	img, err := client.ImageList(context.Background(), types.ImageListOptions{})
	if err != nil {
		return false
	}
	for _, i := range img {
		for _, j := range i.RepoTags {
			if strings.Contains(j, imageName) {
				return true
			}
		}
	}
	return false

}

func PullBusyboxImage() {
	fmt.Println("pulling busybox image")
	client, _ := docker.NewClientWithOpts(docker.FromEnv)
	rd, err := client.ImagePull(context.Background(), BaseImageName, types.ImagePullOptions{})
	if err != nil {
		log.Println(err)
		return
	}
	defer rd.Close()
	buf := make([]byte, 1024)
	for {
		_, err := rd.Read(buf)
		if err != nil {
			break
		}
	}
}

func checkMount(f string) (isDir bool, mp string) {
	if strings.Contains(f, "/") {
		b := path.Base(f)
		return true, fmt.Sprintf("/%s", b)
	} else {
		return false, fmt.Sprintf("/%s", f)
	}
}

func EmptyVolume(volume []string) error {
	image := BaseImageName
	client, _ := docker.NewClientWithOpts(docker.FromEnv)
	var mounts []mount.Mount
	cmd := ""
	for i, v := range volume {
		isDir, mp := checkMount(v)
		if isDir {
			mounts = append(mounts, mount.Mount{
				Type:   mount.TypeBind,
				Source: v,
				Target: mp,
			})

		} else {
			mounts = append(mounts, mount.Mount{
				Type:   mount.TypeVolume,
				Source: v,
				Target: mp,
			})
		}

		if i == 0 {
			cmd = fmt.Sprintf("rm -rf %s/*", mp)
		} else {
			cmd = fmt.Sprintf("%s && rm -rf %s/*", cmd, mp)
		}
	}

	log.Println(cmd)

	cb, err := client.ContainerCreate(context.Background(), &container.Config{
		Image: image,
		Cmd:   []string{"sh", "-c", cmd},
	},
		&container.HostConfig{
			AutoRemove: true,
			Mounts:     mounts,
		}, nil, nil, utils.RandStr(10))

	if err != nil {
		fmt.Println("error creating container: ", err)
		return err
	}
	err = client.ContainerStart(context.Background(), cb.ID, types.ContainerStartOptions{})
	if err != nil {
		fmt.Println("error starting container: ", err)
		return err
	}
	return nil
}
