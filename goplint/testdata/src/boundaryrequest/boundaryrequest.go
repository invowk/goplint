// SPDX-License-Identifier: MPL-2.0

package boundaryrequest

import "errors"

func opaqueGuard(value bool) bool { return value }

func unrelatedError(enabled bool) error {
	if enabled {
		return errors.New("unrelated")
	}
	return nil
}

func wrapError(err error) error { return err }

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

type PointerRequest struct {
	Name string //goplint:ignore -- fixture keeps request shape simple.
}

func (r *PointerRequest) Validate() error {
	if r == nil || r.Name == "" {
		return errors.New("empty pointer request")
	}
	return nil
}

type ComplexOptions struct {
	Name  string   //goplint:ignore -- fixture keeps options shape simple.
	Items []string //goplint:ignore -- fixture keeps options shape simple.
}

func (o ComplexOptions) Validate() error {
	if opaqueGuard(o.Name == "") {
		return errors.New("empty complex options")
	}
	for _, item := range o.Items {
		if item == "" {
			return errors.New("empty item")
		}
	}
	return nil
}

type Service struct{}

var escapedRequest any

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

func (s *Service) AliasSafe(req Request) error {
	alias := &req
	if err := alias.Validate(); err != nil {
		return err
	}
	_ = req.Name
	return nil
}

func (s *Service) AliasUnsafe(req Request) error { // want `parameter "req" of boundaryrequest.Request is used before checked Validate\(\) at exported boundary`
	alias := &req
	_ = alias.Name
	if err := req.Validate(); err != nil {
		return err
	}
	return nil
}

func (s *Service) UnknownEffect(req Request) error { // want UnknownEffect:"protocol-summary:v5:boundaryrequest:\\(\\*boundaryrequest.Service\\).UnknownEffect:1" `parameter "req" of boundaryrequest.Request has inconclusive checked Validate\(\) analysis at exported boundary`
	escapedRequest = &req
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

func (s *Service) ImplicitPointerReceiverSafe(req PointerRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	_ = req.Name
	return nil
}

func (s *Service) ComplexSafe(opts ComplexOptions, enabled bool) error {
	if opaqueGuard(enabled) {
		enabled = false
	}
	if err := opts.Validate(); err != nil {
		return err
	}
	_ = opts.Name
	return nil
}

func (s *Service) UnrelatedFailureThenValidate(opts RunOptions, enabled bool) error {
	if err := unrelatedError(enabled); err != nil {
		return wrapError(err)
	}
	if err := opts.Validate(); err != nil {
		return wrapError(err)
	}
	_ = opts.Name
	return nil
}

func consumeValidatedOptions(opts RunOptions) error {
	_ = opts.Name
	return nil
}

func (s *Service) ValidateThenDelegate(opts RunOptions) error {
	if err := opts.Validate(); err != nil {
		return err
	}
	return consumeValidatedOptions(opts)
}

func (s *Service) NotRequest(opts NotBoundary) {
	_ = opts.Name
}

func (s *Service) unexported(req Request) {
	_ = req.Name
}
