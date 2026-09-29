package store

import (
	"cmp"
	"context"
	"slices"
	"time"

	"gorm.io/gorm"
)

// RegistryRef identifies a registry in listings.
type RegistryRef struct {
	ID   string
	Name string
}

// Package is a flatpak (app or runtime) identified by kind and flatpak ID.
// It groups the images of that flatpak across architectures, branches, tags
// and registries.
type Package struct {
	Kind      string
	FlatpakID string
	// Name, Summary and Version are those of the newest variant (Newer);
	// they may be empty.
	Name    string
	Summary string
	Version string
	// Architectures are the distinct OCI architectures, sorted.
	Architectures []string
	// Branches are the distinct branches, sorted.
	Branches []string
	// Registries provide the package, sorted by name.
	Registries []RegistryRef
	ImageCount int
	// IconImageID is the newest variant with an icon, "" if none.
	IconImageID string
	// HasExtraData is set when any variant downloads extra data on install.
	HasExtraData bool
	// Updated is the creation time of the newest variant, if known.
	Updated *time.Time
}

// PackageFilter restricts ListPackages. A package is listed when any of its
// images matches; its summary always covers all its images.
type PackageFilter struct {
	// RegistryID "" means all registries.
	RegistryID string
	// Query is a case-insensitive substring of the flatpak ID, name or summary.
	Query string
	// Kind is KindApp, KindRuntime or empty.
	Kind string
	// Architecture is an OCI architecture, or empty.
	Architecture string
}

func (f PackageFilter) apply(q *gorm.DB) *gorm.DB {
	if f.RegistryID != "" {
		q = q.Where("images.registry_id = ?", f.RegistryID)
	}
	if f.Kind != "" {
		q = q.Where("images.kind = ?", f.Kind)
	}
	if f.Architecture != "" {
		q = q.Where("images.architecture = ?", f.Architecture)
	}
	if f.Query != "" {
		cond, args := likeAny(f.Query, "images.flatpak_id", "images.name", "images.summary")
		q = q.Where(cond, args...)
	}
	return q
}

// packageKey identifies a package.
type packageKey struct {
	Kind      string
	FlatpakID string
}

// ListPackages returns one page of the packages matching f, ordered by
// display name (the name of the newest variant, falling back to the flatpak
// ID) case-insensitively, then flatpak ID and kind, and the total number of
// matches. Paging happens over the distinct (kind, flatpak ID) pairs in
// SQL; the summaries are then filled from all images of the selected
// packages in one query.
func (s *Store) ListPackages(ctx context.Context, f PackageFilter, p Page) ([]*Package, int, error) {
	p = p.Normalize()
	grouped := func(sel string) *gorm.DB {
		return f.apply(s.ctx(ctx).Model(&Image{}).Select(sel)).Group("images.kind, images.flatpak_id")
	}
	var total int64
	if err := s.ctx(ctx).Raw("SELECT COUNT(*) FROM (?) AS pkgs", grouped("1")).Scan(&total).Error; err != nil {
		return nil, 0, err
	}
	// The display name is the name of the package's newest image (over all
	// its images, like the summary), computed per group.
	newestName := "(SELECT n.name FROM images n WHERE n.kind = images.kind AND n.flatpak_id = images.flatpak_id ORDER BY " +
		s.newestFirst("n") + " LIMIT 1)"
	sel := "images.kind AS kind, images.flatpak_id AS flatpak_id, LOWER(COALESCE(NULLIF(" + newestName + ", ''), images.flatpak_id)) AS display_name"
	var keys []packageKey
	if err := s.ctx(ctx).Table("(?) AS pkgs", grouped(sel)).Select("pkgs.kind, pkgs.flatpak_id").
		Order(s.orderText("pkgs.display_name") + ", " + s.orderText("pkgs.flatpak_id") + ", " + s.orderText("pkgs.kind")).
		Limit(p.Size).Offset(p.Offset).Scan(&keys).Error; err != nil {
		return nil, 0, err
	}
	out := make([]*Package, 0, len(keys))
	if len(keys) == 0 {
		return out, int(total), nil
	}

	byKind := map[string][]string{}
	for _, k := range keys {
		byKind[k.Kind] = append(byKind[k.Kind], k.FlatpakID)
	}
	var cond *gorm.DB
	for kind, ids := range byKind {
		c := s.ctx(ctx).Where("images.kind = ? AND images.flatpak_id IN ?", kind, ids)
		if cond == nil {
			cond = c
		} else {
			cond = cond.Or(c)
		}
	}
	var rows []*Image
	if err := s.images(ctx, false).Where(cond).Order(s.variantOrder()).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	variants := map[packageKey][]*Image{}
	for _, img := range rows {
		k := packageKey{img.Kind, img.FlatpakID}
		variants[k] = append(variants[k], img)
	}
	for _, k := range keys {
		if imgs := variants[k]; len(imgs) > 0 {
			out = append(out, summarizePackage(imgs))
		}
	}
	return out, int(total), nil
}

// variantOrder orders the images of a package by branch, architecture,
// registry name, repository and id (byte order).
func (s *Store) variantOrder() string {
	return s.orderText("images.branch") + ", " + s.orderText("images.architecture") + ", " +
		s.orderText("registries.name") + ", " + s.orderText("images.repository") + ", " + s.orderText("images.id")
}

// GetPackage returns a package and all its images (without labels), ordered
// by branch, architecture, registry name and repository.
func (s *Store) GetPackage(ctx context.Context, kind, flatpakID string) (*Package, []*Image, error) {
	var rows []*Image
	if err := s.images(ctx, false).Where("images.kind = ? AND images.flatpak_id = ?", kind, flatpakID).
		Order(s.variantOrder()).Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 {
		return nil, nil, ErrNotFound
	}
	return summarizePackage(rows), rows, nil
}

// summarizePackage builds the summary of one package from all its images.
func summarizePackage(imgs []*Image) *Package {
	pkg := &Package{
		Kind: imgs[0].Kind, FlatpakID: imgs[0].FlatpakID,
		Architectures: []string{}, Branches: []string{}, Registries: []RegistryRef{},
		ImageCount: len(imgs),
	}
	var newest, icon *Image
	regs := map[string]string{}
	for _, img := range imgs {
		if newest == nil || Newer(img, newest) {
			newest = img
		}
		if img.HasIcon && (icon == nil || Newer(img, icon)) {
			icon = img
		}
		if img.HasExtraData {
			pkg.HasExtraData = true
		}
		if !slices.Contains(pkg.Architectures, img.Architecture) {
			pkg.Architectures = append(pkg.Architectures, img.Architecture)
		}
		if !slices.Contains(pkg.Branches, img.Branch) {
			pkg.Branches = append(pkg.Branches, img.Branch)
		}
		regs[img.RegistryID] = img.RegistryName
	}
	pkg.Name, pkg.Summary, pkg.Version = newest.Name, newest.Summary, newest.Version
	pkg.Updated = utcPtr(newest.Created)
	if icon != nil {
		pkg.IconImageID = icon.ID
	}
	slices.Sort(pkg.Architectures)
	slices.Sort(pkg.Branches)
	for id, name := range regs {
		pkg.Registries = append(pkg.Registries, RegistryRef{ID: id, Name: name})
	}
	slices.SortFunc(pkg.Registries, func(a, b RegistryRef) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
	})
	return pkg
}

// RefProviders returns, for each of the given full refs, the registries
// other than excludeRegistryID that have an image with that ref indexed,
// sorted by name. Refs nobody provides are absent from the result.
func (s *Store) RefProviders(ctx context.Context, refs []string, excludeRegistryID string) (map[string][]RegistryRef, error) {
	out := map[string][]RegistryRef{}
	if len(refs) == 0 {
		return out, nil
	}
	type row struct {
		Ref  string
		ID   string
		Name string
	}
	var rows []row
	q := s.ctx(ctx).Model(&Image{}).Distinct("images.ref AS ref, registries.id AS id, registries.name AS name").
		Joins("JOIN registries ON registries.id = images.registry_id").
		Where("images.ref IN ?", refs)
	if excludeRegistryID != "" {
		q = q.Where("images.registry_id <> ?", excludeRegistryID)
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.Ref] = append(out[r.Ref], RegistryRef{ID: r.ID, Name: r.Name})
	}
	for _, regs := range out {
		slices.SortFunc(regs, func(a, b RegistryRef) int {
			return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
		})
	}
	return out, nil
}
