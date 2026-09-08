let database;
function db() {
  if (!database)
    database = new Promise((resolve, reject) => {
      const request = indexedDB.open("durlim-article-drafts", 1);
      request.onupgradeneeded = () =>
        request.result.createObjectStore("drafts", { keyPath: "key" });
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
    });
  return database;
}
export async function readLocal(key) {
  const database = await db();
  return new Promise((resolve, reject) => {
    const request = database
      .transaction("drafts")
      .objectStore("drafts")
      .get(key);
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}
export async function writeLocal(record) {
  const database = await db();
  return new Promise((resolve, reject) => {
    const tx = database.transaction("drafts", "readwrite");
    tx.objectStore("drafts").put(structuredClone(record));
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error);
    tx.onabort = () => reject(tx.error);
  });
}
export async function allLocal() {
  const database = await db();
  return new Promise((resolve, reject) => {
    const request = database
      .transaction("drafts")
      .objectStore("drafts")
      .getAll();
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}
