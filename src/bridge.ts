export interface ConstructorBridge {
  request(input: RequestInfo | URL, init?: RequestInit): Promise<Response>
}

declare global {
  interface Window {
    constructorBridge?: ConstructorBridge
  }
}

const browserBridge: ConstructorBridge = {
  request: (input, init) => globalThis.fetch(input, init),
}

let installedBridge: ConstructorBridge | undefined

export function installConstructorBridge(bridge: ConstructorBridge): () => void {
  const previousBridge = installedBridge
  installedBridge = bridge
  return () => {
    installedBridge = previousBridge
  }
}

export function constructorRequest(
  input: RequestInfo | URL,
  init?: RequestInit,
): Promise<Response> {
  const bridge = installedBridge ?? globalThis.window?.constructorBridge ?? browserBridge
  return bridge.request(input, init)
}
