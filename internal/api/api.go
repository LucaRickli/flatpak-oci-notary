// Package api implements the Connect admin API (proto/notary/v1) used by the UI.
package api

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"connectrpc.com/connect"
	"github.com/rs/zerolog"

	"github.com/lucarickli/flatpak-oci-notary/internal/auth"
	"github.com/lucarickli/flatpak-oci-notary/internal/flatpakindex"
	"github.com/lucarickli/flatpak-oci-notary/internal/flatpakmeta"
	notaryv1 "github.com/lucarickli/flatpak-oci-notary/internal/gen/notary/v1"
	"github.com/lucarickli/flatpak-oci-notary/internal/gen/notary/v1/notaryv1connect"
	"github.com/lucarickli/flatpak-oci-notary/internal/indexer"
	"github.com/lucarickli/flatpak-oci-notary/internal/store"
	"github.com/lucarickli/flatpak-oci-notary/internal/version"
)

type ctxKey int

const baseURLKey ctxKey = iota

// WithBaseURL stores the public base URL of the current request in ctx.
func WithBaseURL(ctx context.Context, baseURL string) context.Context {
	return context.WithValue(ctx, baseURLKey, baseURL)
}

func baseURL(ctx context.Context) string {
	s, _ := ctx.Value(baseURLKey).(string)
	return s
}

// API implements all Connect services.
type API struct {
	store *store.Store
	// indexer runs syncs in this process; nil when they run in a separate
	// "notary sync" process, in which case syncs are only requested.
	indexer *indexer.Indexer
	auth    *auth.Authenticator // nil when authentication is disabled
	log     zerolog.Logger
}

// New builds the API. x is nil when this server does not run the sync
// scheduler itself (see GetInfoResponse.embedded_sync).
func New(s *store.Store, x *indexer.Indexer, a *auth.Authenticator, log zerolog.Logger) *API {
	return &API{store: s, indexer: x, auth: a, log: log.With().Str("component", "api").Logger()}
}

// embeddedSync reports whether syncs run in this process.
func (a *API) embeddedSync() bool { return a.indexer != nil }

// startSync starts the requested sync of a registry right away when the
// embedded syncer runs; otherwise the request recorded in the store waits
// for the external syncer.
func (a *API) startSync(ctx context.Context, registryID string) {
	if a.indexer != nil {
		a.indexer.Trigger(ctx, registryID)
	}
}

// Handler returns the HTTP handler serving all services below /api.
func (a *API) Handler() http.Handler {
	opts := []connect.HandlerOption{connect.WithInterceptors(a.authInterceptor())}
	mux := http.NewServeMux()
	mux.Handle(notaryv1connect.NewSystemServiceHandler(systemService{a}, opts...))
	mux.Handle(notaryv1connect.NewRegistryServiceHandler(registryService{a}, opts...))
	mux.Handle(notaryv1connect.NewImageServiceHandler(imageService{a}, opts...))
	mux.Handle(notaryv1connect.NewRepositoryServiceHandler(repositoryService{a}, opts...))
	return http.StripPrefix("/api", mux)
}

var publicProcedures = []string{
	notaryv1connect.SystemServiceGetInfoProcedure,
	notaryv1connect.SystemServiceGetMeProcedure,
}

func (a *API) authInterceptor() connect.Interceptor {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if a.auth != nil && !slices.Contains(publicProcedures, req.Spec().Procedure) {
				if a.userFromHeader(req.Header()) == nil {
					return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("sign in required"))
				}
			}
			return next(ctx, req)
		}
	})
}

func (a *API) userFromHeader(h http.Header) *auth.User {
	if a.auth == nil {
		return nil
	}
	return a.auth.UserFromRequest(&http.Request{Header: h})
}

// Authorized reports whether a plain HTTP request may access admin data.
func (a *API) Authorized(r *http.Request) bool {
	return a.auth == nil || a.auth.UserFromRequest(r) != nil
}

// toConnectError maps store errors to Connect codes.
func (a *API) toConnectError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, store.ErrConflict):
		return connect.NewError(connect.CodeAlreadyExists, errors.New(strings.TrimPrefix(err.Error(), store.ErrConflict.Error()+": ")))
	case errors.Is(err, context.Canceled):
		return connect.NewError(connect.CodeCanceled, err)
	default:
		a.log.Error().Err(err).Msg("internal error")
		return connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}
}

func invalid(format string, args ...any) error {
	return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf(format, args...))
}

// requiredID parses a UUID field of a request. A malformed id is an invalid
// argument (never an internal error); a well-formed but unknown one is
// reported as not found by the lookup it is used in.
func requiredID(field, value string) (string, error) {
	id, err := store.ParseID(value)
	if err != nil {
		return "", invalid("%s: %v", field, err)
	}
	return id, nil
}

// optionalID is requiredID for filters, where empty means "all".
func optionalID(field, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	return requiredID(field, value)
}

// textField trims a free-text request field (a search query, a filter or a
// lookup key). A NUL byte is an invalid argument: stored values never
// contain one (see store.CleanText), and Postgres rejects it in parameters,
// which would otherwise surface as an internal error. It is rejected rather
// than stripped, so "a\x00b" never matches "ab". (Invalid UTF-8 cannot
// arrive: protobuf rejects it when decoding.)
func textField(field, value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.IndexByte(value, 0) >= 0 {
		return "", invalid("%s must not contain NUL characters", field)
	}
	return value, nil
}

// textFields applies textField to the fields in place, reporting the first
// invalid one.
func textFields(fields ...namedText) error {
	for _, f := range fields {
		clean, err := textField(f.name, *f.value)
		if err != nil {
			return err
		}
		*f.value = clean
	}
	return nil
}

type namedText struct {
	name  string
	value *string
}

// noNUL rejects NUL characters in stored input fields (see textField).
func noNUL(field string, values ...string) error {
	for _, v := range values {
		if strings.IndexByte(v, 0) >= 0 {
			return invalid("%s must not contain NUL characters", field)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// SystemService

type systemService struct{ *API }

func (s systemService) GetInfo(ctx context.Context, _ *connect.Request[notaryv1.GetInfoRequest]) (*connect.Response[notaryv1.GetInfoResponse], error) {
	return connect.NewResponse(&notaryv1.GetInfoResponse{
		Version:      version.Version,
		PublicUrl:    baseURL(ctx),
		AuthEnabled:  s.auth != nil,
		EmbeddedSync: s.embeddedSync(),
	}), nil
}

func (s systemService) GetMe(_ context.Context, req *connect.Request[notaryv1.GetMeRequest]) (*connect.Response[notaryv1.GetMeResponse], error) {
	if s.auth == nil {
		return connect.NewResponse(&notaryv1.GetMeResponse{Authenticated: true, AuthEnabled: false}), nil
	}
	u := s.userFromHeader(req.Header())
	if u == nil {
		return connect.NewResponse(&notaryv1.GetMeResponse{Authenticated: false, AuthEnabled: true}), nil
	}
	return connect.NewResponse(&notaryv1.GetMeResponse{
		Authenticated: true,
		AuthEnabled:   true,
		User:          &notaryv1.User{Subject: u.Subject, Email: u.Email, Name: u.Name},
	}), nil
}

func (s systemService) GetOverview(ctx context.Context, _ *connect.Request[notaryv1.GetOverviewRequest]) (*connect.Response[notaryv1.GetOverviewResponse], error) {
	o, err := s.store.Overview(ctx)
	if err != nil {
		return nil, s.toConnectError(err)
	}
	return connect.NewResponse(&notaryv1.GetOverviewResponse{
		Registries:   int32(o.Registries),
		Repositories: int32(o.Repositories),
		Images:       int32(o.Images),
		Apps:         int32(o.Apps),
		Runtimes:     int32(o.Runtimes),
	}), nil
}

func (s systemService) Logout(_ context.Context, _ *connect.Request[notaryv1.LogoutRequest]) (*connect.Response[notaryv1.LogoutResponse], error) {
	resp := connect.NewResponse(&notaryv1.LogoutResponse{})
	if s.auth != nil {
		s.auth.ClearSession(resp.Header())
	}
	return resp, nil
}

// ---------------------------------------------------------------------------
// RegistryService

type registryService struct{ *API }

const maxSyncIntervalMinutes = 7 * 24 * 60

func (s registryService) registryFromInput(in *notaryv1.RegistryInput) (*store.Registry, error) {
	if in == nil {
		return nil, invalid("registry is required")
	}
	name := strings.TrimSpace(in.GetName())
	if name == "" || len(name) > 100 {
		return nil, invalid("name must be between 1 and 100 characters")
	}
	u, err := indexer.NormalizeRegistryURL(in.GetUrl())
	if err != nil {
		return nil, invalid("%v", err)
	}
	if in.GetSyncIntervalMinutes() < 0 || in.GetSyncIntervalMinutes() > maxSyncIntervalMinutes {
		return nil, invalid("sync interval must be between 0 and %d minutes", maxSyncIntervalMinutes)
	}
	authType := store.AuthAnonymous
	if in.GetAuthType() == notaryv1.AuthType_AUTH_TYPE_BASIC {
		authType = store.AuthBasic
	}
	r := &store.Registry{
		Name:                name,
		URL:                 u,
		Insecure:            in.GetInsecure(),
		AuthType:            authType,
		UseCatalog:          in.GetUseCatalog(),
		Repositories:        cleanList(in.GetRepositories(), true),
		RepositoryPatterns:  cleanList(in.GetRepositoryPatterns(), false),
		TagPatterns:         cleanList(in.GetTagPatterns(), false),
		SyncIntervalMinutes: int(in.GetSyncIntervalMinutes()),
	}
	if authType == store.AuthBasic {
		r.Username = strings.TrimSpace(in.GetUsername())
		r.Password = in.GetPassword()
	}
	if !r.UseCatalog && len(r.Repositories) == 0 {
		return nil, invalid("enable catalog discovery or list at least one repository")
	}
	for _, err := range []error{
		noNUL("name", r.Name),
		noNUL("username", r.Username),
		noNUL("password", r.Password),
		noNUL("repositories", r.Repositories...),
		noNUL("repository patterns", r.RepositoryPatterns...),
		noNUL("tag patterns", r.TagPatterns...),
	} {
		if err != nil {
			return nil, err
		}
	}
	return r, nil
}

func cleanList(in []string, trimSlashes bool) []string {
	out := []string{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if trimSlashes {
			s = strings.Trim(s, "/")
		}
		if s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

func (s registryService) ListRegistries(ctx context.Context, _ *connect.Request[notaryv1.ListRegistriesRequest]) (*connect.Response[notaryv1.ListRegistriesResponse], error) {
	regs, err := s.store.ListRegistries(ctx)
	if err != nil {
		return nil, s.toConnectError(err)
	}
	out := make([]*notaryv1.Registry, len(regs))
	for i, r := range regs {
		out[i] = registryToProto(r)
	}
	return connect.NewResponse(&notaryv1.ListRegistriesResponse{Registries: out}), nil
}

func (s registryService) GetRegistry(ctx context.Context, req *connect.Request[notaryv1.GetRegistryRequest]) (*connect.Response[notaryv1.GetRegistryResponse], error) {
	id, err := requiredID("id", req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	r, err := s.store.GetRegistry(ctx, id)
	if err != nil {
		return nil, s.toConnectError(err)
	}
	return connect.NewResponse(&notaryv1.GetRegistryResponse{Registry: registryToProto(r)}), nil
}

func (s registryService) CreateRegistry(ctx context.Context, req *connect.Request[notaryv1.CreateRegistryRequest]) (*connect.Response[notaryv1.CreateRegistryResponse], error) {
	in, err := s.registryFromInput(req.Msg.GetRegistry())
	if err != nil {
		return nil, err
	}
	in.SyncRequested = true // index it right away
	r, err := s.store.CreateRegistry(ctx, in)
	if err != nil {
		return nil, s.toConnectError(err)
	}
	s.log.Info().Str("registry_id", r.ID).Str("registry", r.Name).Msg("registry created")
	s.startSync(ctx, r.ID)
	if r, err = s.store.GetRegistry(ctx, r.ID); err != nil {
		return nil, s.toConnectError(err)
	}
	return connect.NewResponse(&notaryv1.CreateRegistryResponse{Registry: registryToProto(r)}), nil
}

func (s registryService) UpdateRegistry(ctx context.Context, req *connect.Request[notaryv1.UpdateRegistryRequest]) (*connect.Response[notaryv1.UpdateRegistryResponse], error) {
	in, err := s.registryFromInput(req.Msg.GetRegistry())
	if err != nil {
		return nil, err
	}
	if in.ID, err = requiredID("id", req.Msg.GetId()); err != nil {
		return nil, err
	}
	// The password is write-only: unset keeps it, switching to anonymous clears it.
	setPassword := req.Msg.GetRegistry().Password != nil || in.AuthType == store.AuthAnonymous
	in.SyncRequested = true // the change may select other images
	r, err := s.store.UpdateRegistry(ctx, in, setPassword)
	if err != nil {
		return nil, s.toConnectError(err)
	}
	s.log.Info().Str("registry_id", r.ID).Str("registry", r.Name).Msg("registry updated")
	s.startSync(ctx, r.ID)
	if r, err = s.store.GetRegistry(ctx, r.ID); err != nil {
		return nil, s.toConnectError(err)
	}
	return connect.NewResponse(&notaryv1.UpdateRegistryResponse{Registry: registryToProto(r)}), nil
}

func (s registryService) DeleteRegistry(ctx context.Context, req *connect.Request[notaryv1.DeleteRegistryRequest]) (*connect.Response[notaryv1.DeleteRegistryResponse], error) {
	id, err := requiredID("id", req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	if err := s.store.DeleteRegistry(ctx, id); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("registry is used by at least one repository; delete or move those first"))
		}
		return nil, s.toConnectError(err)
	}
	if s.indexer != nil {
		s.indexer.Forget(id)
	}
	s.log.Info().Str("registry_id", id).Msg("registry deleted")
	return connect.NewResponse(&notaryv1.DeleteRegistryResponse{}), nil
}

func (s registryService) SyncRegistry(ctx context.Context, req *connect.Request[notaryv1.SyncRegistryRequest]) (*connect.Response[notaryv1.SyncRegistryResponse], error) {
	id, err := requiredID("id", req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	// The request is recorded first: with the embedded syncer the claim
	// below clears it at once; without one, or while another instance is
	// syncing the registry, it stays and the next syncer run picks it up.
	if err := s.store.RequestSync(ctx, id); err != nil {
		return nil, s.toConnectError(err)
	}
	s.startSync(ctx, id)
	r, err := s.store.GetRegistry(ctx, id)
	if err != nil {
		return nil, s.toConnectError(err)
	}
	if r.SyncState != store.SyncSyncing {
		s.log.Info().Str("registry_id", r.ID).Str("registry", r.Name).Bool("embedded_sync", s.embeddedSync()).Msg("sync requested")
	}
	return connect.NewResponse(&notaryv1.SyncRegistryResponse{Registry: registryToProto(r)}), nil
}

func (s registryService) TestRegistry(ctx context.Context, req *connect.Request[notaryv1.TestRegistryRequest]) (*connect.Response[notaryv1.TestRegistryResponse], error) {
	in := req.Msg.GetRegistry()
	if in == nil {
		return nil, invalid("registry is required")
	}
	u, err := indexer.NormalizeRegistryURL(in.GetUrl())
	if err != nil {
		return connect.NewResponse(&notaryv1.TestRegistryResponse{Error: err.Error()}), nil
	}
	r := &store.Registry{
		URL:          u,
		Insecure:     in.GetInsecure(),
		AuthType:     store.AuthAnonymous,
		UseCatalog:   in.GetUseCatalog(),
		Repositories: cleanList(in.GetRepositories(), true),
	}
	if in.GetAuthType() == notaryv1.AuthType_AUTH_TYPE_BASIC {
		r.AuthType = store.AuthBasic
		r.Username = strings.TrimSpace(in.GetUsername())
		r.Password = in.GetPassword()
		if in.Password == nil && req.Msg.Id != nil {
			id, err := requiredID("id", req.Msg.GetId())
			if err != nil {
				return nil, err
			}
			existing, err := s.store.GetRegistry(ctx, id)
			if err != nil {
				return nil, s.toConnectError(err)
			}
			r.Password = existing.Password
		}
	}
	res := indexer.TestRegistry(ctx, r)
	return connect.NewResponse(&notaryv1.TestRegistryResponse{
		Ok:                 res.OK,
		Error:              res.Error,
		CatalogSupported:   res.CatalogSupported,
		SampleRepositories: res.SampleRepositories,
	}), nil
}

// ---------------------------------------------------------------------------
// ImageService

type imageService struct{ *API }

// page converts the proto pagination fields; the store applies the
// defaults and limits documented in the proto.
func page(size, offset int32) store.Page {
	return store.Page{Size: int(size), Offset: int(offset)}
}

// kindFilter maps the proto kind to the store's; unspecified means all.
func kindFilter(k notaryv1.RefKind) string {
	switch k {
	case notaryv1.RefKind_REF_KIND_APP:
		return store.KindApp
	case notaryv1.RefKind_REF_KIND_RUNTIME:
		return store.KindRuntime
	default:
		return ""
	}
}

func (s imageService) ListImages(ctx context.Context, req *connect.Request[notaryv1.ListImagesRequest]) (*connect.Response[notaryv1.ListImagesResponse], error) {
	registryID, err := optionalID("registry_id", req.Msg.GetRegistryId())
	if err != nil {
		return nil, err
	}
	f := store.ImageFilter{
		RegistryID:   registryID,
		Query:        req.Msg.GetQuery(),
		Kind:         kindFilter(req.Msg.GetKind()),
		FlatpakID:    req.Msg.GetFlatpakId(),
		Architecture: req.Msg.GetArchitecture(),
	}
	if err := textFields(namedText{"query", &f.Query}, namedText{"flatpak_id", &f.FlatpakID}, namedText{"architecture", &f.Architecture}); err != nil {
		return nil, err
	}
	images, total, err := s.store.ListImages(ctx, f, page(req.Msg.GetPageSize(), req.Msg.GetOffset()))
	if err != nil {
		return nil, s.toConnectError(err)
	}
	return connect.NewResponse(&notaryv1.ListImagesResponse{Images: imagesToProto(images), TotalSize: int32(total)}), nil
}

func (s imageService) ListImageRepositories(ctx context.Context, req *connect.Request[notaryv1.ListImageRepositoriesRequest]) (*connect.Response[notaryv1.ListImageRepositoriesResponse], error) {
	registryID, err := optionalID("registry_id", req.Msg.GetRegistryId())
	if err != nil {
		return nil, err
	}
	query, err := textField("query", req.Msg.GetQuery())
	if err != nil {
		return nil, err
	}
	f := store.ImageRepositoryFilter{RegistryID: registryID, Query: query}
	repos, total, err := s.store.ListImageRepositories(ctx, f, page(req.Msg.GetPageSize(), req.Msg.GetOffset()))
	if err != nil {
		return nil, s.toConnectError(err)
	}
	out := make([]*notaryv1.ImageRepository, len(repos))
	for i, r := range repos {
		out[i] = imageRepositoryToProto(r)
	}
	return connect.NewResponse(&notaryv1.ListImageRepositoriesResponse{Repositories: out, TotalSize: int32(total)}), nil
}

func (s imageService) ListPackages(ctx context.Context, req *connect.Request[notaryv1.ListPackagesRequest]) (*connect.Response[notaryv1.ListPackagesResponse], error) {
	registryID, err := optionalID("registry_id", req.Msg.GetRegistryId())
	if err != nil {
		return nil, err
	}
	f := store.PackageFilter{
		RegistryID:   registryID,
		Query:        req.Msg.GetQuery(),
		Kind:         kindFilter(req.Msg.GetKind()),
		Architecture: req.Msg.GetArchitecture(),
	}
	if err := textFields(namedText{"query", &f.Query}, namedText{"architecture", &f.Architecture}); err != nil {
		return nil, err
	}
	pkgs, total, err := s.store.ListPackages(ctx, f, page(req.Msg.GetPageSize(), req.Msg.GetOffset()))
	if err != nil {
		return nil, s.toConnectError(err)
	}
	out := make([]*notaryv1.Package, len(pkgs))
	for i, p := range pkgs {
		out[i] = packageToProto(p)
	}
	return connect.NewResponse(&notaryv1.ListPackagesResponse{Packages: out, TotalSize: int32(total)}), nil
}

func (s imageService) GetPackage(ctx context.Context, req *connect.Request[notaryv1.GetPackageRequest]) (*connect.Response[notaryv1.GetPackageResponse], error) {
	kind := kindFilter(req.Msg.GetKind())
	if kind == "" {
		return nil, invalid("kind is required")
	}
	flatpakID := strings.TrimSpace(req.Msg.GetFlatpakId())
	if flatpakID == "" {
		return nil, invalid("flatpak_id is required")
	}
	if strings.IndexByte(flatpakID, 0) >= 0 {
		// No stored flatpak id contains NUL (and Postgres rejects it as a
		// parameter): such a package cannot exist. Not found rather than
		// invalid, so a mangled package URL shows "package not found".
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("package %q not found", flatpakID))
	}
	pkg, variants, err := s.store.GetPackage(ctx, kind, flatpakID)
	if err != nil {
		return nil, s.toConnectError(err)
	}
	return connect.NewResponse(&notaryv1.GetPackageResponse{Package: packageToProto(pkg), Variants: imagesToProto(variants)}), nil
}

func (s imageService) GetImage(ctx context.Context, req *connect.Request[notaryv1.GetImageRequest]) (*connect.Response[notaryv1.GetImageResponse], error) {
	id, err := requiredID("id", req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	img, err := s.store.GetImage(ctx, id)
	if err != nil {
		return nil, s.toConnectError(err)
	}
	return connect.NewResponse(&notaryv1.GetImageResponse{
		Image:    imageToProto(img),
		Labels:   strippedLabels(img.Labels),
		Metadata: img.Labels[flatpakmeta.Label],
	}), nil
}

// ServeIcon serves GET /icons/{id}: the appstream icon of an image.
func (a *API) ServeIcon(w http.ResponseWriter, r *http.Request) {
	if !a.Authorized(r) {
		http.Error(w, "sign in required", http.StatusUnauthorized)
		return
	}
	id, err := store.ParseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	img, err := a.store.GetImage(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	for _, k := range store.IconLabels {
		v := img.Labels[k]
		if v == "" {
			continue
		}
		if strings.HasPrefix(v, "https://") || strings.HasPrefix(v, "http://") {
			http.Redirect(w, r, v, http.StatusFound)
			return
		}
		meta, data, ok := strings.Cut(strings.TrimPrefix(v, "data:"), ",")
		if !ok || !strings.HasSuffix(meta, ";base64") {
			continue
		}
		b, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			continue
		}
		ct := strings.TrimSuffix(meta, ";base64")
		if !strings.HasPrefix(ct, "image/") || strings.Contains(ct, "svg") {
			ct = "application/octet-stream"
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "private, max-age=86400")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(b)
		return
	}
	http.NotFound(w, r)
}

// ---------------------------------------------------------------------------
// RepositoryService

type repositoryService struct{ *API }

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

func (s repositoryService) repositoryFromInput(in *notaryv1.RepositoryInput) (*store.Repository, error) {
	if in == nil {
		return nil, invalid("repository is required")
	}
	slug := strings.TrimSpace(in.GetSlug())
	if !slugRe.MatchString(slug) || strings.HasSuffix(slug, ".flatpakrepo") {
		return nil, invalid("slug must match [a-z0-9][a-z0-9._-]* (max 63 characters)")
	}
	if strings.TrimSpace(in.GetRegistryId()) == "" {
		return nil, invalid("registry is required")
	}
	registryID, err := requiredID("registry_id", in.GetRegistryId())
	if err != nil {
		return nil, err
	}
	homepage := strings.TrimSpace(in.GetHomepage())
	if homepage != "" && !strings.HasPrefix(homepage, "https://") && !strings.HasPrefix(homepage, "http://") {
		return nil, invalid("homepage must be an http(s) URL")
	}
	sources := make([]store.Source, 0, len(in.GetSources()))
	for _, src := range in.GetSources() {
		s := store.Source{
			RepositoryPattern: strings.TrimSpace(src.GetRepositoryPattern()),
			RefPattern:        strings.TrimSpace(src.GetRefPattern()),
			TagPattern:        strings.TrimSpace(src.GetTagPattern()),
			Exclude:           src.GetExclude(),
		}
		if err := noNUL("sources", s.RepositoryPattern, s.RefPattern, s.TagPattern); err != nil {
			return nil, err
		}
		sources = append(sources, s)
	}
	if err := noNUL("title, description and homepage", in.GetTitle(), in.GetDescription(), homepage); err != nil {
		return nil, err
	}
	return &store.Repository{
		Slug:        slug,
		Title:       strings.TrimSpace(in.GetTitle()),
		Description: strings.TrimSpace(in.GetDescription()),
		Homepage:    homepage,
		RegistryID:  registryID,
		Sources:     sources,
	}, nil
}

// registryImages loads images per registry for counting and previews.
type registryImages struct {
	s     *store.Store
	cache map[string][]*store.Image
}

func newRegistryImages(s *store.Store) *registryImages {
	return &registryImages{s: s, cache: map[string][]*store.Image{}}
}

func (ri *registryImages) get(ctx context.Context, registryID string) ([]*store.Image, error) {
	if images, ok := ri.cache[registryID]; ok {
		return images, nil
	}
	images, err := ri.s.RegistryImages(ctx, registryID, false)
	if err != nil {
		return nil, err
	}
	ri.cache[registryID] = images
	return images, nil
}

func (s repositoryService) toProto(ctx context.Context, ri *registryImages, p *store.Repository) (*notaryv1.Repository, error) {
	images, err := ri.get(ctx, p.RegistryID)
	if err != nil {
		return nil, err
	}
	n := 0
	for _, img := range images {
		if flatpakindex.MatchSources(p.Sources, img) {
			n++
		}
	}
	return repositoryToProto(p, baseURL(ctx), n), nil
}

func (s repositoryService) one(ctx context.Context, p *store.Repository) (*notaryv1.Repository, error) {
	out, err := s.toProto(ctx, newRegistryImages(s.store), p)
	if err != nil {
		return nil, s.toConnectError(err)
	}
	return out, nil
}

func (s repositoryService) ListRepositories(ctx context.Context, _ *connect.Request[notaryv1.ListRepositoriesRequest]) (*connect.Response[notaryv1.ListRepositoriesResponse], error) {
	repos, err := s.store.ListRepositories(ctx)
	if err != nil {
		return nil, s.toConnectError(err)
	}
	ri := newRegistryImages(s.store)
	out := make([]*notaryv1.Repository, len(repos))
	for i, p := range repos {
		if out[i], err = s.toProto(ctx, ri, p); err != nil {
			return nil, s.toConnectError(err)
		}
	}
	return connect.NewResponse(&notaryv1.ListRepositoriesResponse{Repositories: out}), nil
}

func (s repositoryService) GetRepository(ctx context.Context, req *connect.Request[notaryv1.GetRepositoryRequest]) (*connect.Response[notaryv1.GetRepositoryResponse], error) {
	id, err := requiredID("id", req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	p, err := s.store.GetRepository(ctx, id)
	if err != nil {
		return nil, s.toConnectError(err)
	}
	out, err := s.one(ctx, p)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&notaryv1.GetRepositoryResponse{Repository: out}), nil
}

func (s repositoryService) CreateRepository(ctx context.Context, req *connect.Request[notaryv1.CreateRepositoryRequest]) (*connect.Response[notaryv1.CreateRepositoryResponse], error) {
	in, err := s.repositoryFromInput(req.Msg.GetRepository())
	if err != nil {
		return nil, err
	}
	p, err := s.store.CreateRepository(ctx, in)
	if err != nil {
		return nil, s.toConnectError(err)
	}
	s.log.Info().Str("repository_id", p.ID).Str("slug", p.Slug).Msg("repository created")
	out, err := s.one(ctx, p)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&notaryv1.CreateRepositoryResponse{Repository: out}), nil
}

func (s repositoryService) UpdateRepository(ctx context.Context, req *connect.Request[notaryv1.UpdateRepositoryRequest]) (*connect.Response[notaryv1.UpdateRepositoryResponse], error) {
	in, err := s.repositoryFromInput(req.Msg.GetRepository())
	if err != nil {
		return nil, err
	}
	if in.ID, err = requiredID("id", req.Msg.GetId()); err != nil {
		return nil, err
	}
	p, err := s.store.UpdateRepository(ctx, in)
	if err != nil {
		return nil, s.toConnectError(err)
	}
	s.log.Info().Str("repository_id", p.ID).Str("slug", p.Slug).Msg("repository updated")
	out, err := s.one(ctx, p)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&notaryv1.UpdateRepositoryResponse{Repository: out}), nil
}

func (s repositoryService) DeleteRepository(ctx context.Context, req *connect.Request[notaryv1.DeleteRepositoryRequest]) (*connect.Response[notaryv1.DeleteRepositoryResponse], error) {
	id, err := requiredID("id", req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	if err := s.store.DeleteRepository(ctx, id); err != nil {
		return nil, s.toConnectError(err)
	}
	s.log.Info().Str("repository_id", id).Msg("repository deleted")
	return connect.NewResponse(&notaryv1.DeleteRepositoryResponse{}), nil
}

func (s repositoryService) PreviewRepository(ctx context.Context, req *connect.Request[notaryv1.PreviewRepositoryRequest]) (*connect.Response[notaryv1.PreviewRepositoryResponse], error) {
	id, err := requiredID("id", req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	p, err := s.store.GetRepository(ctx, id)
	if err != nil {
		return nil, s.toConnectError(err)
	}
	images, err := s.store.RegistryImages(ctx, p.RegistryID, false)
	if err != nil {
		return nil, s.toConnectError(err)
	}
	var f flatpakindex.Filter
	if a := strings.TrimSpace(req.Msg.GetArchitecture()); a != "" {
		f.Architectures = []string{a}
	}
	if t := strings.TrimSpace(req.Msg.GetTag()); t != "" {
		f.Tags = []string{t}
	}
	// The selection (per-ref de-duplication) needs all images, so paginate
	// the computed result in memory.
	selected := flatpakindex.Select(p.Sources, f, images)
	pg := store.Slice(selected, page(req.Msg.GetPageSize(), req.Msg.GetOffset()))

	// Runtimes the selection needs but does not serve, and which other
	// registries could provide them (over the whole selection, not the page).
	missing := flatpakindex.MissingRuntimes(selected)
	refs := make([]string, len(missing))
	for i, m := range missing {
		refs[i] = store.KindRuntime + "/" + m.Runtime
	}
	providers, err := s.store.RefProviders(ctx, refs, p.RegistryID)
	if err != nil {
		return nil, s.toConnectError(err)
	}
	return connect.NewResponse(&notaryv1.PreviewRepositoryResponse{
		Images:          imagesToProto(pg),
		TotalSize:       int32(len(selected)),
		MissingRuntimes: missingRuntimesToProto(missing, providers),
	}), nil
}
