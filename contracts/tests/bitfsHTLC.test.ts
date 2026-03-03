import { BitfsHTLC } from '../src/contracts/bitfsHTLC'
import {
    bsv,
    ByteString,
    MethodCallOptions,
    PubKey,
    Ripemd160,
    sha256,
    Sha256,
    Sig,
    TestWallet,
    toByteString,
    toHex,
    DummyProvider,
} from 'scrypt-ts'

// Helpers for building public key hashes
function pubKeyHash(pub: bsv.PublicKey): Ripemd160 {
    return Ripemd160(toHex(bsv.crypto.Hash.sha256ripemd160(pub.toBuffer())))
}

describe('BitfsHTLC', () => {
    const AMOUNT = 10_000 // satoshis locked in contract

    let seller: bsv.PrivateKey
    let buyer: bsv.PrivateKey
    let capsule: ByteString
    let capsuleHash: Sha256
    let invoiceId: ByteString

    beforeAll(async () => {
        await BitfsHTLC.loadArtifact()

        seller = bsv.PrivateKey.fromRandom(bsv.Networks.testnet)
        buyer = bsv.PrivateKey.fromRandom(bsv.Networks.testnet)

        // 32-byte capsule (64 hex chars)
        capsule = toByteString('aa'.repeat(32))
        capsuleHash = sha256(capsule)

        // 16-byte invoice ID (32 hex chars)
        invoiceId = toByteString('bb'.repeat(16))
    })

    /**
     * Build a dummy deploy tx that locks `amount` sats to the contract locking script.
     */
    function buildDeployTx(instance: BitfsHTLC): bsv.Transaction {
        const tx = new bsv.Transaction()
        // Add a dummy funding input (coinbase-like)
        tx.addInput(
            new bsv.Transaction.Input({
                prevTxId:
                    '0000000000000000000000000000000000000000000000000000000000000000',
                outputIndex: 0,
                script: bsv.Script.empty(),
            }),
            // dummy output script
            bsv.Script.buildPublicKeyHashOut(seller.toAddress()),
            AMOUNT + 1000 // enough for the contract output
        )
        // Contract output
        tx.addOutput(
            new bsv.Transaction.Output({
                script: instance.lockingScript,
                satoshis: AMOUNT,
            })
        )
        return tx
    }

    /**
     * Build a spending tx that consumes the contract output.
     * Sets instance.to and returns the tx.
     */
    function buildSpendingTx(
        instance: BitfsHTLC,
        deployTx: bsv.Transaction,
        lockTime: number = 0
    ): bsv.Transaction {
        const tx = new bsv.Transaction()
        tx.addInput(
            new bsv.Transaction.Input({
                prevTxId: deployTx.id,
                outputIndex: 0,
                script: bsv.Script.empty(),
            }),
            instance.lockingScript,
            AMOUNT
        )
        // Need a dummy output for the spending tx
        tx.addOutput(
            new bsv.Transaction.Output({
                script: bsv.Script.buildPublicKeyHashOut(
                    seller.toAddress()
                ),
                satoshis: AMOUNT - 500,
            })
        )
        tx.nLockTime = lockTime
        // Set sequence < UINT_MAX for timeLock to work
        tx.inputs[0].sequenceNumber = 0xfffffffe
        return tx
    }

    /**
     * Produce a Sig from a private key for the given tx/input.
     */
    function signTx(
        tx: bsv.Transaction,
        privKey: bsv.PrivateKey,
        lockingScript: bsv.Script,
        inputIndex: number = 0
    ): Sig {
        const sighashType =
            bsv.crypto.Signature.SIGHASH_ALL |
            bsv.crypto.Signature.SIGHASH_FORKID
        const preimage = bsv.Transaction.Sighash.sighashPreimage(
            tx,
            sighashType,
            inputIndex,
            lockingScript,
            new bsv.crypto.BN(AMOUNT)
        )
        const hashBuf = bsv.crypto.Hash.sha256sha256(preimage)
        const sig = bsv.crypto.ECDSA.sign(hashBuf, privKey)
        sig.nhashtype = sighashType
        return Sig(toHex(sig.toTxFormat()))
    }

    // ──────────────────── claim() ────────────────────

    describe('claim', () => {
        it('should succeed with correct capsule preimage and seller sig', () => {
            const instance = new BitfsHTLC(
                invoiceId,
                capsuleHash,
                pubKeyHash(seller.publicKey),
                pubKeyHash(buyer.publicKey),
                72n
            )
            const deployTx = buildDeployTx(instance)
            const spendTx = buildSpendingTx(instance, deployTx)

            instance.to = { tx: spendTx, inputIndex: 0 }
            const sig = signTx(spendTx, seller, instance.lockingScript)
            const result = instance.verify((self) => {
                self.claim(capsule, sig, PubKey(toHex(seller.publicKey)))
            })
            expect(result.success).toBe(true)
        })

        it('should fail with wrong capsule preimage', () => {
            const instance = new BitfsHTLC(
                invoiceId,
                capsuleHash,
                pubKeyHash(seller.publicKey),
                pubKeyHash(buyer.publicKey),
                72n
            )
            const deployTx = buildDeployTx(instance)
            const spendTx = buildSpendingTx(instance, deployTx)

            instance.to = { tx: spendTx, inputIndex: 0 }
            const sig = signTx(spendTx, seller, instance.lockingScript)
            const wrongCapsule = toByteString('cc'.repeat(32))

            expect(() => {
                instance.verify((self) => {
                    self.claim(
                        wrongCapsule,
                        sig,
                        PubKey(toHex(seller.publicKey))
                    )
                })
            }).toThrow()
        })

        it('should fail with wrong seller pubkey (buyer key used)', () => {
            const instance = new BitfsHTLC(
                invoiceId,
                capsuleHash,
                pubKeyHash(seller.publicKey),
                pubKeyHash(buyer.publicKey),
                72n
            )
            const deployTx = buildDeployTx(instance)
            const spendTx = buildSpendingTx(instance, deployTx)

            instance.to = { tx: spendTx, inputIndex: 0 }
            // Sign with buyer's key — wrong identity for seller path
            const sig = signTx(spendTx, buyer, instance.lockingScript)

            expect(() => {
                instance.verify((self) => {
                    self.claim(
                        capsule,
                        sig,
                        PubKey(toHex(buyer.publicKey))
                    )
                })
            }).toThrow()
        })
    })

    // ──────────────────── refund() ────────────────────

    describe('refund', () => {
        it('should succeed after timeout with buyer sig', () => {
            const instance = new BitfsHTLC(
                invoiceId,
                capsuleHash,
                pubKeyHash(seller.publicKey),
                pubKeyHash(buyer.publicKey),
                72n // timeout = block 72
            )
            const deployTx = buildDeployTx(instance)
            // lockTime = 100 > timeout = 72
            const spendTx = buildSpendingTx(instance, deployTx, 100)

            instance.to = { tx: spendTx, inputIndex: 0 }
            const sig = signTx(spendTx, buyer, instance.lockingScript)
            const result = instance.verify((self) => {
                self.refund(sig, PubKey(toHex(buyer.publicKey)))
            })
            expect(result.success).toBe(true)
        })

        it('should fail before timeout', () => {
            const instance = new BitfsHTLC(
                invoiceId,
                capsuleHash,
                pubKeyHash(seller.publicKey),
                pubKeyHash(buyer.publicKey),
                72n // timeout = block 72
            )
            const deployTx = buildDeployTx(instance)
            // lockTime = 50 < timeout = 72
            const spendTx = buildSpendingTx(instance, deployTx, 50)

            instance.to = { tx: spendTx, inputIndex: 0 }
            const sig = signTx(spendTx, buyer, instance.lockingScript)

            expect(() => {
                instance.verify((self) => {
                    self.refund(sig, PubKey(toHex(buyer.publicKey)))
                })
            }).toThrow()
        })

        it('should fail with wrong buyer pubkey (seller key used)', () => {
            const instance = new BitfsHTLC(
                invoiceId,
                capsuleHash,
                pubKeyHash(seller.publicKey),
                pubKeyHash(buyer.publicKey),
                72n
            )
            const deployTx = buildDeployTx(instance)
            // lockTime past timeout
            const spendTx = buildSpendingTx(instance, deployTx, 100)

            instance.to = { tx: spendTx, inputIndex: 0 }
            // Sign with seller's key — wrong identity for buyer path
            const sig = signTx(spendTx, seller, instance.lockingScript)

            expect(() => {
                instance.verify((self) => {
                    self.refund(sig, PubKey(toHex(seller.publicKey)))
                })
            }).toThrow()
        })
    })
})
