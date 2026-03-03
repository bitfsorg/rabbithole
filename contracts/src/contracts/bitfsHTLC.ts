import {
    assert,
    ByteString,
    hash160,
    method,
    prop,
    PubKey,
    PubKeyHash,
    sha256,
    Sha256,
    Sig,
    SmartContract,
} from 'scrypt-ts'

export class BitfsHTLC extends SmartContract {
    // 16-byte invoice ID — mandatory, ensures unique script hash per payment
    @prop()
    readonly invoiceId: ByteString

    // SHA256(capsule) — the hash lock
    @prop()
    readonly capsuleHash: Sha256

    // Seller's public key hash (20 bytes)
    @prop()
    readonly sellerPkh: PubKeyHash

    // Buyer's public key hash (20 bytes)
    @prop()
    readonly buyerPkh: PubKeyHash

    // Refund timeout (block height)
    @prop()
    readonly timeout: bigint

    constructor(
        invoiceId: ByteString,
        capsuleHash: Sha256,
        sellerPkh: PubKeyHash,
        buyerPkh: PubKeyHash,
        timeout: bigint
    ) {
        super(...arguments)
        this.invoiceId = invoiceId
        this.capsuleHash = capsuleHash
        this.sellerPkh = sellerPkh
        this.buyerPkh = buyerPkh
        this.timeout = timeout
    }

    // Seller claims by revealing the capsule preimage
    @method()
    public claim(preimage: ByteString, sig: Sig, pubkey: PubKey) {
        // Verify hash lock
        assert(sha256(preimage) == this.capsuleHash, 'wrong capsule')
        // Verify seller identity
        assert(hash160(pubkey) == this.sellerPkh, 'wrong seller')
        // Verify signature
        assert(this.checkSig(sig, pubkey), 'invalid sig')
    }

    // Buyer refunds after timeout (on-chain, no seller cooperation needed)
    @method()
    public refund(sig: Sig, pubkey: PubKey) {
        // Verify timeout has passed (uses OP_PUSH_TX for nLockTime check)
        assert(this.timeLock(this.timeout), 'too early')
        // Verify buyer identity
        assert(hash160(pubkey) == this.buyerPkh, 'wrong buyer')
        // Verify signature
        assert(this.checkSig(sig, pubkey), 'invalid sig')
    }
}
