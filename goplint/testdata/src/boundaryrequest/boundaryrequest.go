// SPDX-License-Identifier: MPL-2.0

package boundaryrequest

import "errors"

type Request struct {
	Name  string //goplint:ignore -- fixture keeps request shape simple.
	Image string //goplint:ignore -- fixture keeps request shape simple.
}

func (r Request) Validate() error {
	if r.Name == "" && r.Image == "" {
		return errors.New("empty request")
	}
	return nil
}

type RunOptions struct {
	Name string //goplint:ignore -- fixture keeps options shape simple.
}

func (o RunOptions) Validate() error {
	if o.Name == "" {
		return errors.New("empty options")
	}
	return nil
}

type NotBoundary struct {
	Name string //goplint:ignore -- fixture keeps non-boundary shape simple.
}

func (n NotBoundary) Validate() error { return nil }

type Service struct{}

func (s *Service) Unsafe(req Request) error { // want `parameter "req" of boundaryrequest.Request is used before checked Validate\(\) at exported boundary`
	_ = req.Name
	if err := req.Validate(); err != nil {
		return err
	}
	return nil
}

func (s *Service) Safe(req Request) error {
	if err := req.Validate(); err != nil {
		return err
	}
	_ = req.Name
	return nil
}

func (s *Service) DefaultThenValidate(req Request) error {
	if req.Image == "" {
		req.Image = "debian:stable-slim"
	}
	if err := req.Validate(); err != nil {
		return err
	}
	_ = req.Image
	return nil
}

func (s *Service) UnsafeBranch(req Request) error { // want `parameter "req" of boundaryrequest.Request is used before checked Validate\(\) at exported boundary`
	if req.Name != "" {
		return nil
	}
	if err := req.Validate(); err != nil {
		return err
	}
	return nil
}

//goplint:trusted-boundary -- caller validates before dispatch.
func (s *Service) Trusted(req Request) error {
	_ = req.Name
	return nil
}

func (s *Service) OptionsUnsafe(opts RunOptions) error { // want `parameter "opts" of boundaryrequest.RunOptions is used before checked Validate\(\) at exported boundary`
	_ = opts.Name
	return nil
}

func (s *Service) PointerSafe(req *Request) error {
	if err := req.Validate(); err != nil {
		return err
	}
	_ = req.Name
	return nil
}

func (s *Service) NotRequest(opts NotBoundary) {
	_ = opts.Name
}

func (s *Service) unexported(req Request) {
	_ = req.Name
}
