# Doge Key Manager

The DKM holds your encrypted master key and generates (derives) private-public
keypairs for pups and other parts of the DogeBox ecosystem.

## Key Store

Keys are encrypted at rest with the DogeBox password and stored on disk.

Passwords are first hashed using Argon2 memory-hard KDF (Argon2id variant)
with parameters time=3, memory=64M, threads=4 and the BLAKE2b hash function
as recommended in RFC 9106.

The password-derived hash is then used to encrypt the master key with
ChaCha20 cypher and Poly1305 Authenticated Encryption (AE) scheme.

Keys in DKM are only in memory while they are actively being used for
Authentication or key derivation.

