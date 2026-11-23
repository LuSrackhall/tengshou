// 合同台账岛里的扫码签名对话框接线：打开前先取合同最新详情（与迁移前
// openSignatureDialog 一致），没有甲方/乙方档案时按旧提示拦截；对话框
// 本身由 SignatureDialog 渲染。
import { ref, type Ref } from "vue";
import { getContract } from "../../api/endpoints";
import type { Contract, ContractDetail } from "../../api/types";
import { errorMessage, toast } from "../../bridge/app";

export interface SignatureRequest {
  key: number;
  contract: Contract;
  detail: ContractDetail;
}

export interface SignatureDialogState {
  signRequest: Ref<SignatureRequest | null>;
  open(contract: Contract): Promise<void>;
  close(): void;
}

export function useSignatureDialog(): SignatureDialogState {
  const signRequest = ref<SignatureRequest | null>(null);
  let seq = 0;

  async function open(contract: Contract): Promise<void> {
    try {
      const detail = await getContract(contract.id);
      if (!detail.party_a && !detail.party_b) {
        toast("合同还没有关联相对方，无法发起签名", true);
        return;
      }
      signRequest.value = { key: ++seq, contract, detail };
    } catch (error) {
      toast(errorMessage(error), true);
    }
  }

  function close(): void {
    signRequest.value = null;
  }

  return { signRequest, open, close };
}
