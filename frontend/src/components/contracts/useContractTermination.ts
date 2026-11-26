// 合同生命周期接线（te-x54oj）：表单「已终止」三件套、续签来源加载与
// 起租早于原到期日时的本地预确认。
//
// 预确认只在来源合同仍 active 时进行：服务端也只在来源 active 时联动
// （internal/service/contracts.go buildContract）；续签联动完成后再保存该
// 续签合同（来源已 terminated）不应反复弹确认（build-basic 评审 S2）。
import { ref } from "vue";
import { getContract } from "../../api/endpoints";
import type { Contract, ContractStatus } from "../../api/types";
import type { ContractInputExtras } from "./form";
import { earlyRenewalTerminationPrompt } from "./lease";

/** 预确认所需的最小来源信息：续签草稿与已加载的来源合同都满足。 */
export interface RenewalTerminationSource {
  contract_no: string;
  status: ContractStatus;
  lease_end?: string;
}

/** 编辑续签合同时加载来源合同，供横幅展示与早终止预确认。 */
export function useRenewalSource(contract: () => Contract | null) {
  const renewalSource = ref<Contract | null>(null);
  const renewalSourceMissing = ref(false);

  async function loadRenewalSource(): Promise<void> {
    const sourceId = contract()?.source_contract_id;
    if (!sourceId) {
      return;
    }
    try {
      renewalSource.value = (await getContract(sourceId)).contract;
    } catch {
      renewalSourceMissing.value = true;
    }
  }

  return { renewalSource, renewalSourceMissing, loadRenewalSource };
}

export function useContractTermination(contract: () => Contract | null) {
  // 「已终止」附加字段：编辑时带出，新建时为空；离开该状态仍随表单提交，
  // 由服务端清空。
  const terminatedOn = ref(contract()?.terminated_on ?? "");
  const terminationReason = ref(contract()?.termination_reason ?? "");
  const terminationNote = ref(contract()?.termination_note ?? "");

  /** collectContractInput 的终止/续签确认扩展。 */
  function inputExtras(renewalConfirmed: boolean): Pick<
    ContractInputExtras,
    "terminatedOn" | "terminationReason" | "terminationNote" | "renewalTerminationConfirm"
  > {
    return {
      terminatedOn: terminatedOn.value,
      terminationReason: terminationReason.value,
      terminationNote: terminationNote.value,
      renewalTerminationConfirm: renewalConfirmed,
    };
  }

  /**
   * 续签起租日早于来源到期日时的本地预确认。
   * 返回 true：需要确认且用户已确认；false：无需确认；null：用户取消保存。
   * 来源不是 active 时跳过，服务端也不会联动。
   */
  function confirmEarlyRenewal(
    leaseStart: string,
    source: RenewalTerminationSource | null,
  ): boolean | null {
    if (!source || source.status !== "active") {
      return false;
    }
    const prompt = earlyRenewalTerminationPrompt(
      leaseStart,
      source.lease_end ?? "",
      source.contract_no,
    );
    if (!prompt) {
      return false;
    }
    return window.confirm(prompt) ? true : null;
  }

  return {
    terminatedOn,
    terminationReason,
    terminationNote,
    inputExtras,
    confirmEarlyRenewal,
  };
}
