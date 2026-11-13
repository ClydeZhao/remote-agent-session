import { createServer } from 'node:net'
import { chmod, lstat, mkdir, unlink, writeFile } from 'node:fs/promises'
import { homedir } from 'node:os'
import { isAbsolute, join } from 'node:path'
import { createAnswerReceipts } from './answer-receipts.mjs'
import { BridgeError, createBridge } from './bridge.mjs'

export const name = 'remote-agent-session-relay'
export const inject = ['agents', 'userQuestions', 'sandboxPolicy', 'profileContext', 'llm']

export async function apply(ctx, config = {}) {
  const directory = config.directory ?? join(process.env.DSH_HOME || join(homedir(), '.dsh'), 'remote-agent-session')
  if (!isAbsolute(directory)) throw new BridgeError('invalid_directory')
  await mkdir(directory, { recursive: true, mode: 0o700 })
  const stat = await lstat(directory)
  if (!stat.isDirectory() || stat.isSymbolicLink() || (stat.mode & 0o077) !== 0 || stat.uid !== process.getuid()) throw new BridgeError('unsafe_directory')
  const receiptsDirectory = join(directory, 'answers')
  await mkdir(receiptsDirectory, { recursive: true, mode: 0o700 })
  const bridge = createBridge(ctx, undefined, createAnswerReceipts(receiptsDirectory))
  const socketPath = join(directory, bridge.instance.slice(0, 8) + '.sock')
  const descriptorPath = join(directory, bridge.instance + '.json')
  const sockets = new Set()
  const server = createServer(socket => {
    if (sockets.size >= 16) return socket.destroy()
    sockets.add(socket)
    socket.on('close', () => sockets.delete(socket))
    socket.on('error', () => {})
    let buffer = Buffer.alloc(0)
    let pending = 0
    socket.setTimeout(60000, () => socket.destroy())
    socket.on('data', chunk => {
      buffer = Buffer.concat([buffer, chunk])
      if (buffer.length > 1048576) return socket.destroy()
      let end
      while ((end = buffer.indexOf(10)) >= 0) {
        const line = buffer.subarray(0, end)
        buffer = buffer.subarray(end + 1)
        if (++pending > 8) return socket.destroy()
        let request
        try {
          request = JSON.parse(line.toString('utf8'))
          if (!Number.isSafeInteger(request.id) || !request.params || typeof request.method !== 'string') throw new Error()
        } catch {
          return socket.destroy()
        }
        Promise.resolve().then(() => bridge.dispatch(request.method, request.params)).then(result => {
          const output = JSON.stringify({ id: request.id, result }) + '\n'
          if (Buffer.byteLength(output) > 8388608) throw new BridgeError('history_too_large')
          if (!socket.destroyed) socket.write(output)
        }).catch(error => {
          if (!socket.destroyed) socket.write(JSON.stringify({ id: request.id, error: { code: -32000, message: error instanceof BridgeError ? error.code : 'runtime_error' } }) + '\n')
        }).finally(() => { pending-- })
      }
    })
  })
  await new Promise((resolve, reject) => {
    server.once('error', reject)
    server.listen(socketPath, resolve)
  })
  await chmod(socketPath, 0o600)
  await writeFile(descriptorPath, JSON.stringify({ protocol: 1, runtime: 'deepseek-harness', instance: bridge.instance, pid: process.pid, socket: socketPath }), { mode: 0o600, flag: 'wx' })
  ctx.effect(() => async () => {
    bridge.dispose()
    for (const socket of sockets) socket.destroy()
    await new Promise(resolve => server.close(resolve))
    await unlink(socketPath).catch(() => {})
    await unlink(descriptorPath).catch(() => {})
  })
}
