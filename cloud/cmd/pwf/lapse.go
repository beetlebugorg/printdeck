package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslambda "github.com/aws/aws-sdk-go-v2/service/lambda"

	"printdeck/cloud/api"
)

type lapseOptions struct {
	Local, List bool
	Parallel    int
}

// runLapseBackfill makes the video of the time lapse of each print that has frames and no video
// (api/lapse.go), and deletes its frames. By default the function of the stack makes each video,
// a few prints at a time. With -local, this computer makes them with its ffmpeg, and the frames
// come down from the bucket.
func runLapseBackfill(ctx context.Context, cfg aws.Config, stack string, opt lapseOptions) error {
	out, err := stackOutputs(ctx, cfg, stack)
	if err != nil {
		return err
	}
	st, err := dataStore(ctx, cfg, stack)
	if err != nil {
		return err
	}
	_, dirs, err := st.ListDir(ctx, "x/")
	if err != nil {
		return err
	}
	type job struct{ project, print string }
	var jobs []job
	for _, d := range dirs {
		project := strings.TrimSuffix(strings.TrimPrefix(d, "x/"), "/")
		prints, err := api.LapsePrints(ctx, st, project)
		if err != nil {
			return fmt.Errorf("the prints of %s: %w", project, err)
		}
		for _, p := range prints {
			jobs = append(jobs, job{project, p})
		}
	}
	fmt.Printf("%d prints have frames and no video\n", len(jobs))
	if opt.List || len(jobs) == 0 {
		return nil
	}
	var run func(ctx context.Context, project, print string) error
	if opt.Local {
		bin, err := exec.LookPath("ffmpeg")
		if err != nil {
			return errors.New("-local needs ffmpeg on the PATH")
		}
		s := &api.Server{Store: st, Base: out["Url"], Lapse: api.FFmpeg(bin)}
		run = s.LapseVideo
	} else {
		lc := awslambda.NewFromConfig(cfg)
		name := stack + "-api"
		run = func(ctx context.Context, project, print string) error {
			b, err := json.Marshal(map[string]any{"pwf": "job", "job": api.Job{Kind: "lapse", Project: project, Print: print}})
			if err != nil {
				return err
			}
			// A call that waits for the end of the job, so that only a few run at the same time.
			res, err := lc.Invoke(ctx, &awslambda.InvokeInput{FunctionName: &name, Payload: b})
			if err != nil {
				return err
			}
			if res.FunctionError != nil {
				return fmt.Errorf("the function: %s", aws.ToString(res.FunctionError))
			}
			left, err := api.LapsePrints(ctx, st, project)
			if err != nil {
				return err
			}
			for _, p := range left {
				if p == print {
					return errors.New("no video: see the logs of the function")
				}
			}
			return nil
		}
	}
	sem := make(chan struct{}, max(1, opt.Parallel))
	var wg sync.WaitGroup
	var mu sync.Mutex
	failed := 0
	for _, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			err := run(ctx, j.project, j.print)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
				fmt.Printf("%s/%s: %v\n", j.project, j.print, err)
				return
			}
			fmt.Printf("%s/%s: the video is up, and the frames are gone\n", j.project, j.print)
		}()
	}
	wg.Wait()
	if failed > 0 {
		return fmt.Errorf("%d of %d prints have no video", failed, len(jobs))
	}
	return nil
}
