package data

import "context"

// ModelSpecRow 模型扩展规格（持久化在 model_specs 表）。
type ModelSpecRow struct {
	Model              string
	MaxTokens          int
	PricingIn          string
	PricingOut         string
	License            string
	InputModalities    string // 逗号分隔
	ReleaseDate        string
	CardURL            string
	Architecture       string // v0.9：架构（HF 富化，如 LlamaForCausalLM）
	SupportedInterfaces string // v0.9：支持接口（chat/embeddings/rerank，逗号分隔）
	SyncedAt           int64
}

const modelSpecColumns = `model, max_tokens, pricing_in, pricing_out, license, input_modalities,
	release_date, card_url, architecture, supported_interfaces, synced_at`

// UpsertModelSpec 写入/覆盖某模型的扩展规格。
func (s *Store) UpsertModelSpec(ctx context.Context, r ModelSpecRow) error {
	r.SyncedAt = now()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO model_specs (`+modelSpecColumns+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(model) DO UPDATE SET
			max_tokens=excluded.max_tokens, pricing_in=excluded.pricing_in, pricing_out=excluded.pricing_out,
			license=excluded.license, input_modalities=excluded.input_modalities, release_date=excluded.release_date,
			card_url=excluded.card_url, architecture=excluded.architecture,
			supported_interfaces=excluded.supported_interfaces, synced_at=excluded.synced_at`,
		r.Model, r.MaxTokens, r.PricingIn, r.PricingOut, r.License, r.InputModalities,
		r.ReleaseDate, r.CardURL, r.Architecture, r.SupportedInterfaces, r.SyncedAt)
	return err
}

// ListModelSpecs 返回全部模型扩展规格（modelID → ModelSpecRow）。
func (s *Store) ListModelSpecs(ctx context.Context) (map[string]ModelSpecRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+modelSpecColumns+` FROM model_specs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]ModelSpecRow)
	for rows.Next() {
		var r ModelSpecRow
		if err := rows.Scan(&r.Model, &r.MaxTokens, &r.PricingIn, &r.PricingOut, &r.License,
			&r.InputModalities, &r.ReleaseDate, &r.CardURL, &r.Architecture,
			&r.SupportedInterfaces, &r.SyncedAt); err != nil {
			return nil, err
		}
		out[r.Model] = r
	}
	return out, rows.Err()
}
