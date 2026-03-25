-- name: CreateUser :one
INSERT INTO users (email,
                   password,
                   full_name,
                   refresh_token,
                   token_expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING
    id,
    email,
    full_name,
    refresh_token,
    created_at;

-- name: GetUserById :one
SELECT *
FROM users
WHERE id = $1
  AND deleted_at IS NULL FOR UPDATE;

-- name: GetUserByEmail :one
SELECT *
FROM users
WHERE email = $1
  AND deleted_at IS NULL;

-- name: GetUserByName :many
SELECT *
FROM users
WHERE full_name % $1
  AND deleted_at IS NULL
ORDER BY similarity(full_name, $1) DESC;

-- name: GetUserByRefreshToken :one
SELECT *
FROM users
WHERE refresh_token = $1
  AND deleted_at IS NULL;

-- name: UpdateUserDetails :one
UPDATE users
SET full_name = $2,
    email     = $3
WHERE id = $1
  AND deleted_at IS NULL
RETURNING *;

-- name: UpdateRefreshToken :exec
UPDATE users
SET refresh_token    = $2,
    token_expires_at = $3
WHERE id = $1
  AND deleted_at IS NULL;


-- name: RevokeRefreshToken :exec
UPDATE users
SET refresh_token    = NULL,
    token_expires_at = NULL
WHERE id = $1
  AND deleted_at IS NULL;

-- name: SoftDeleteUser :exec
UPDATE users
SET deleted_at = NOW()
WHERE id = $1
  AND deleted_at IS NULL;
